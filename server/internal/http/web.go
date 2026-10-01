package http

import (
	"errors"
	"io/fs"
	"mime"
	stdhttp "net/http"
	"os"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
)

// webFiles 只开放构建壳和 assets；os.Root 阻止目录穿越与逃逸符号链接。
type webFiles struct{ root, assets *os.Root }

func openWeb(directory string) (*webFiles, error) {
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	w := &webFiles{root: r}
	f, err := r.Open("index.html")
	if err == nil {
		var stat fs.FileInfo
		stat, err = f.Stat()
		if err == nil && (!stat.Mode().IsRegular() || stat.Size() > 16<<20) {
			err = errors.New("invalid web index")
		}
		_ = f.Close()
	}
	if err == nil {
		w.assets, err = r.OpenRoot("assets")
	}
	if err != nil {
		w.close()
		return nil, err
	}
	return w, nil
}
func (w *webFiles) close() {
	if w.assets != nil {
		_ = w.assets.Close()
	}
	if w.root != nil {
		_ = w.root.Close()
	}
}
func (s *Server) webIndex(c *echo.Context) error {
	return s.web.serve(c, s.web.root, "index.html", false)
}
func (s *Server) webAsset(c *echo.Context) error {
	name := c.Param("*")
	if !fs.ValidPath(name) || strings.Contains(name, "\\") {
		return echo.ErrNotFound
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return echo.ErrNotFound
		}
	}
	switch path.Ext(name) {
	case ".js", ".css", ".svg", ".png", ".jpg", ".jpeg", ".webp", ".ico", ".woff", ".woff2":
	default:
		return echo.ErrNotFound
	}
	return s.web.serve(c, s.web.assets, name, true)
}
func (w *webFiles) serve(c *echo.Context, root *os.Root, name string, immutable bool) error {
	f, err := root.Open(name)
	if err != nil {
		return echo.ErrNotFound
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 16<<20 {
		return echo.ErrNotFound
	}
	if immutable {
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	c.Response().Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	stdhttp.ServeContent(c.Response(), c.Request(), name, stat.ModTime(), f)
	return nil
}
