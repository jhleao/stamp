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
	archive := zip.NewWriter(destination)
	defer archive.Close()
	count := 0
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink %s", rel)
		}
		info, err := entry.Info()
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
	})
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
		if file.FileInfo().IsDir() || file.Mode()&os.ModeSymlink != 0 {
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
