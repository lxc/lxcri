package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {

	root := "/sys/fs/cgroup/kubepods.slice"

	cgControllers, err := loadControllers(root)
	if err != nil {
		panic(err)
	}

	subtreeControl := fmtControllers(cgControllers...)
	print(subtreeControl)

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && strings.HasSuffix(path, ".scope") {
			return filepath.SkipDir
		}
		if !info.IsDir() && info.Name() == "cgroup.subtree_control" {
			println(path)
			if err := os.WriteFile(path, []byte(subtreeControl), 0); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		panic(err)
	}

}

func fmtControllers(controllers ...string) string {
	var b strings.Builder
	for i, c := range controllers {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('+')
		b.WriteString(c)
	}
	b.WriteString("\n")
	return b.String()
}

func loadControllers(cgroupPath string) ([]string, error) {
	// #nosec
	data, err := os.ReadFile(filepath.Join(cgroupPath, "cgroup.controllers"))
	if err != nil {
		return nil, fmt.Errorf("failed to read cgroup.controllers: %s", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), " "), nil
}
