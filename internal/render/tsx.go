package render

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
)

const componentRuntime = `
const Fragment = Symbol("Fragment");
const raw = value => ({ __stampRaw: String(value ?? "") });
let activeMeta = {};
let activeFormat = "";
const h = (type, props, ...children) => ({ type, props: props || {}, children: children.flat(Infinity) });
const escapeHTML = value => String(value).replace(/[&<>"']/g, char => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"})[char]);
const attrName = name => ({className:"class", htmlFor:"for"})[name] || name.replace(/[A-Z]/g, letter => "-" + letter.toLowerCase());
const renderNode = node => {
  if (node == null || node === false || node === true) return "";
  if (node.__stampRaw != null) return node.__stampRaw;
  if (Array.isArray(node)) return node.map(renderNode).join("");
  if (typeof node !== "object") return escapeHTML(node);
  if (node.type === Fragment) return renderNode(node.children);
  if (typeof node.type === "function") return renderNode(node.type({...node.props, children: node.children}));
  let attrs = "";
  for (const [key, value] of Object.entries(node.props || {})) {
    if (key === "children" || key === "dangerouslySetInnerHTML" || value == null || value === false) continue;
    if (/^on/i.test(key)) throw new Error("event handlers are unavailable in Stamp components");
    if (value === true) attrs += " " + attrName(key);
    else attrs += " " + attrName(key) + '="' + escapeHTML(typeof value === "object" ? JSON.stringify(value) : value) + '"';
  }
  const body = node.props?.dangerouslySetInnerHTML?.__html ?? renderNode(node.children);
  return "<" + node.type + attrs + ">" + body + "</" + node.type + ">";
};
const __stampDefineComponent = (name, component) => {
  globalThis[name] = input => component({
    props: input || {},
    meta: activeMeta,
    format: activeFormat,
    children: input?.children || []
  });
};
const __stampRender = (component, props, meta, format, children) => {
  activeMeta = meta;
  activeFormat = format;
  return renderNode(component({props, meta, format, children: raw(children)}));
};
`

type compiledComponent struct {
	name            string
	globalName      string
	code            string
	selfClosing     *regexp.Regexp
	explicitClosing string
}

type componentProgram struct {
	components []compiledComponent
}

// ComponentInfo is optional author-written guidance exported beside a
// component. Stamp surfaces it to agents without introducing a second
// manifest that can drift away from the implementation.
type ComponentInfo struct {
	Name        string
	Description string
	Usage       string
}

type cachedComponentProgram struct {
	fingerprint [sha256.Size]byte
	program     *componentProgram
}

var componentPrograms = struct {
	sync.Mutex
	byRoot map[string]cachedComponentProgram
}{byRoot: make(map[string]cachedComponentProgram)}

func loadComponentProgram(root string) (*componentProgram, error) {
	dir := filepath.Join(root, "theme", "components")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return &componentProgram{}, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	hash := sha256.New()
	type sourceFile struct {
		name string
		data []byte
	}
	var sources []sourceFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(name, ".html.tmpl") {
			return nil, fmt.Errorf("component %s uses the retired template format; rename it to .tsx and export a component", name)
		}
		if !strings.HasSuffix(name, ".tsx") {
			continue
		}
		tag := strings.TrimSuffix(name, ".tsx")
		if !validComponentName(tag) {
			return nil, fmt.Errorf("component %s must use a PascalCase name such as MetricCard", name)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
		sources = append(sources, sourceFile{name: tag, data: data})
	}

	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], hash.Sum(nil))
	canonicalRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	componentPrograms.Lock()
	if cached, ok := componentPrograms.byRoot[canonicalRoot]; ok && cached.fingerprint == fingerprint {
		componentPrograms.Unlock()
		return cached.program, nil
	}
	componentPrograms.Unlock()

	program := &componentProgram{}
	for index, source := range sources {
		globalName := fmt.Sprintf("__stampComponent%d", index)
		code, err := compileComponent(source.name+".tsx", source.data, globalName)
		if err != nil {
			return nil, fmt.Errorf("component %s: %w", source.name, err)
		}
		program.components = append(program.components, compiledComponent{
			name: source.name, globalName: globalName, code: code,
			selfClosing:     regexp.MustCompile(`(?i)<` + regexp.QuoteMeta(source.name) + `([^<>]*?)/\s*>`),
			explicitClosing: `<` + source.name + `$1></` + source.name + `>`,
		})
	}
	componentPrograms.Lock()
	componentPrograms.byRoot[canonicalRoot] = cachedComponentProgram{fingerprint: fingerprint, program: program}
	componentPrograms.Unlock()
	return program, nil
}

func compileComponent(name string, source []byte, globalName string) (string, error) {
	result := api.Transform(string(source), api.TransformOptions{
		Loader: api.LoaderTSX, Format: api.FormatIIFE, GlobalName: globalName,
		JSXFactory: "h", JSXFragment: "Fragment", Target: api.ES2018,
		Sourcefile: name, Sourcemap: api.SourceMapNone,
	})
	if len(result.Errors) > 0 {
		problem := result.Errors[0]
		if problem.Location != nil {
			return "", fmt.Errorf("%s:%d:%d: %s", name, problem.Location.Line, problem.Location.Column+1, problem.Text)
		}
		return "", errors.New(problem.Text)
	}
	return string(result.Code), nil
}

type componentRenderer struct {
	runtime    *goja.Runtime
	render     goja.Callable
	components map[string]goja.Value
	metadata   map[string]ComponentInfo
	program    *componentProgram
}

func (program *componentProgram) newRenderer() (*componentRenderer, error) {
	runtime := goja.New()
	timer := time.AfterFunc(time.Second, func() { runtime.Interrupt("component initialization took too long") })
	defer timer.Stop()
	if _, err := runtime.RunString(componentRuntime); err != nil {
		return nil, err
	}
	render, ok := goja.AssertFunction(runtime.Get("__stampRender"))
	if !ok {
		return nil, errors.New("component runtime did not initialize")
	}
	defineComponent, ok := goja.AssertFunction(runtime.Get("__stampDefineComponent"))
	if !ok {
		return nil, errors.New("component runtime did not initialize")
	}
	result := &componentRenderer{
		runtime: runtime, render: render,
		components: make(map[string]goja.Value, len(program.components)),
		metadata:   make(map[string]ComponentInfo, len(program.components)),
		program:    program,
	}
	for _, component := range program.components {
		if _, err := runtime.RunString(component.code); err != nil {
			return nil, fmt.Errorf("component %s: %w", component.name, err)
		}
		moduleValue := runtime.Get(component.globalName)
		if goja.IsUndefined(moduleValue) || goja.IsNull(moduleValue) {
			return nil, fmt.Errorf("component %s did not initialize", component.name)
		}
		module := moduleValue.ToObject(runtime)
		fn := module.Get("default")
		if _, ok := goja.AssertFunction(fn); !ok {
			return nil, fmt.Errorf("component %s must export a default function", component.name)
		}
		if _, err := defineComponent(goja.Undefined(), runtime.ToValue(component.name), fn); err != nil {
			return nil, fmt.Errorf("component %s: %w", component.name, err)
		}
		key := strings.ToLower(component.name)
		if _, exists := result.components[key]; exists {
			return nil, fmt.Errorf("component name %s conflicts with another component", component.name)
		}
		result.components[key] = fn
		info := ComponentInfo{Name: component.name}
		if metadata := module.Get("metadata"); metadata != nil && !goja.IsUndefined(metadata) && !goja.IsNull(metadata) {
			object := metadata.ToObject(runtime)
			info.Description = metadataString(object.Get("description"))
			info.Usage = metadataString(object.Get("usage"))
		}
		result.metadata[component.name] = info
	}
	return result, nil
}

func metadataString(value goja.Value) string {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return ""
	}
	return strings.TrimSpace(value.String())
}

// ComponentCatalog returns the workspace's components and their optional
// author guidance in stable filename order.
func ComponentCatalog(root string) ([]ComponentInfo, error) {
	program, err := loadComponentProgram(root)
	if err != nil {
		return nil, err
	}
	renderer, err := program.newRenderer()
	if err != nil {
		return nil, err
	}
	result := make([]ComponentInfo, 0, len(program.components))
	for _, component := range program.components {
		result = append(result, renderer.metadata[component.name])
	}
	return result, nil
}

func (renderer *componentRenderer) has(name string) bool {
	_, ok := renderer.components[strings.ToLower(name)]
	return ok
}

func (renderer *componentRenderer) renderComponent(name string, view componentData) (string, error) {
	component, ok := renderer.components[strings.ToLower(name)]
	if !ok {
		return "", fmt.Errorf("unknown component %s", name)
	}
	renderer.runtime.ClearInterrupt()
	timer := time.AfterFunc(250*time.Millisecond, func() { renderer.runtime.Interrupt("component took too long") })
	defer timer.Stop()
	value, err := renderer.render(
		goja.Undefined(),
		component,
		renderer.runtime.ToValue(view.Props),
		renderer.runtime.ToValue(view.Meta),
		renderer.runtime.ToValue(view.Format),
		renderer.runtime.ToValue(view.Content),
	)
	if err != nil {
		return "", err
	}
	return value.String(), nil
}
