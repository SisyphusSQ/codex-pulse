package http

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	stdhttp "net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
)

// webFiles 只开放二进制内嵌的构建壳和 assets，不访问运行目录。
type webFiles struct{ root fs.FS }

func openWeb(files fs.FS) (*webFiles, error) {
	r, err := fs.Sub(files, "dist")
	if err != nil {
		return nil, err
	}
	w := &webFiles{root: r}
	stat, err := fs.Stat(r, "index.html")
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 16<<20 {
		return nil, errors.New("invalid embedded web index")
	}
	stat, err = fs.Stat(r, "assets")
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() {
		return nil, errors.New("invalid embedded web assets")
	}
	return w, nil
}
func (s *Server) webIndex(c *echo.Context) error {
	return s.web.serve(c, "index.html", false)
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
	return s.web.serve(c, "assets/"+name, true)
}
func (w *webFiles) serve(c *echo.Context, name string, immutable bool) error {
	f, err := w.root.Open(name)
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
	content, ok := f.(io.ReadSeeker)
	if !ok {
		return errors.New("embedded web file is not seekable")
	}
	if immutable {
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	c.Response().Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	stdhttp.ServeContent(c.Response(), c.Request(), name, stat.ModTime(), content)
	return nil
}
