package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const staticCacheControl = "public, max-age=3600"

func staticHandler(fsys fs.FS) http.Handler {
	files := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "."
		}
		info, err := fs.Stat(fsys, name)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(&cacheOnOK{ResponseWriter: w}, r)
	})
}

type cacheOnOK struct {
	http.ResponseWriter
	wroteHeader bool
}

func (c *cacheOnOK) WriteHeader(code int) {
	if c.wroteHeader {
		return
	}
	c.wroteHeader = true
	if code == http.StatusOK {
		c.Header().Set("Cache-Control", staticCacheControl)
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *cacheOnOK) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(b)
}

func (c *cacheOnOK) Unwrap() http.ResponseWriter { return c.ResponseWriter }
