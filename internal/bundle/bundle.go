package bundle

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxFiles = 10_000
	maxFile  = 256 << 20
	maxTotal = 512 << 20
)

func PackFile(root, destination string) error {
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	err = Pack(root, file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func Pack(root string, destination io.Writer) error {
	return packWith(root, destination, nil, false)
}

func PackWith(root string, destination io.Writer, extra map[string][]byte) error {
	return packWith(root, destination, extra, false)
}

// PackSource creates a portable source revision without derived outputs.
// Backends upload outputs separately so rendering never dirties a source lease.
func PackSource(root string, destination io.Writer) error {
	return packWith(root, destination, nil, true)
}

func packWith(root string, destination io.Writer, extra map[string][]byte, skipOutputs bool) error {
	archive := zip.NewWriter(destination)
	count := 0
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			if rel == ".stamp" || strings.HasPrefix(rel, ".stamp"+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			if skipOutputs && rel == "outputs" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(path)
			if filepath.ToSlash(rel) != "CLAUDE.md" || readErr != nil || target != "AGENTS.md" {
				return fmt.Errorf("refusing symlink %s", rel)
			}
			count++
			if count > maxFiles {
				return fmt.Errorf("project has more than %d files", maxFiles)
			}
			header := &zip.FileHeader{Name: "CLAUDE.md", Method: zip.Store}
			header.SetMode(os.ModeSymlink | 0o777)
			writer, err := archive.CreateHeader(header)
			if err != nil {
				return err
			}
			_, err = writer.Write([]byte(target))
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Size() > maxFile {
			return fmt.Errorf("%s exceeds 256 MB", rel)
		}
		count++
		if count > maxFiles {
			return fmt.Errorf("project has more than %d files", maxFiles)
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		header.SetMode(0o644)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}); err != nil {
		_ = archive.Close()
		return err
	}
	for name, data := range extra {
		clean := filepath.ToSlash(filepath.Clean(name))
		if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return fmt.Errorf("invalid extra archive path %q", name)
		}
		writer, err := archive.CreateHeader(&zip.FileHeader{Name: clean, Method: zip.Deflate})
		if err != nil {
			_ = archive.Close()
			return err
		}
		if _, err := writer.Write(data); err != nil {
			_ = archive.Close()
			return err
		}
	}
	return archive.Close()
}

func UnpackFile(source, destination string) error {
	archive, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer archive.Close()
	return unpack(archive.File, destination)
}

func UnpackReader(source io.ReaderAt, size int64, destination string) error {
	archive, err := zip.NewReader(source, size)
	if err != nil {
		return err
	}
	return unpack(archive.File, destination)
}

func unpack(files []*zip.File, destination string) error {
	if len(files) > maxFiles {
		return fmt.Errorf("archive has more than %d files", maxFiles)
	}
	var total uint64
	for _, file := range files {
		name := filepath.FromSlash(file.Name)
		if file.FileInfo().IsDir() {
			return fmt.Errorf("archive contains unsupported entry %q", file.Name)
		}
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive path escapes project: %q", file.Name)
		}
		if file.UncompressedSize64 > maxFile {
			return fmt.Errorf("%s exceeds 256 MB", file.Name)
		}
		total += file.UncompressedSize64
		if total > maxTotal {
			return fmt.Errorf("archive exceeds 512 MB")
		}
		target := filepath.Join(destination, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if file.Mode()&os.ModeSymlink != 0 {
			if file.Name != "CLAUDE.md" || file.UncompressedSize64 > 64 {
				return fmt.Errorf("archive contains unsupported symlink %q", file.Name)
			}
			reader, err := file.Open()
			if err != nil {
				return err
			}
			linkTarget, readErr := io.ReadAll(io.LimitReader(reader, 65))
			closeErr := reader.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			if string(linkTarget) != "AGENTS.md" {
				return fmt.Errorf("CLAUDE.md has unsafe symlink target")
			}
			if err := os.Symlink("AGENTS.md", target); err != nil {
				return err
			}
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			reader.Close()
			return err
		}
		_, copyErr := io.Copy(output, io.LimitReader(reader, maxFile+1))
		readCloseErr := reader.Close()
		writeCloseErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if readCloseErr != nil {
			return readCloseErr
		}
		if writeCloseErr != nil {
			return writeCloseErr
		}
	}
	return nil
}
