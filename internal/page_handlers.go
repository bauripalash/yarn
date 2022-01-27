package internal

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/james4k/fmatter"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

const pagesDir = "pages"

//go:embed pages/*.md
var builtinPages embed.FS

type FrontMatter struct {
	Title       string
	Description string
}

type Page struct {
	Content      string
	LastModified time.Time
}

// PageHandler ...
func (s *Server) PageHandler(name string) httprouter.Handle {
	pagesBaseDir := filepath.Join(s.config.Data, pagesDir)

	pageMutex := &sync.RWMutex{}
	pageCache := make(map[string]*Page)

	getPage := func(name string) (*Page, error) {
		fn := filepath.Join(pagesBaseDir, fmt.Sprintf("%s.md", name))

		pageMutex.RLock()
		page, isCached := pageCache[name]
		pageMutex.RUnlock()

		if isCached && FileExists(fn) {
			if fileInfo, err := os.Stat(fn); err == nil {
				if fileInfo.ModTime().After(page.LastModified) {
					data, err := os.ReadFile(fn)
					if err != nil {
						log.WithError(err).Warnf("error reading page %s", name)
						return page, nil
					}
					page.Content = string(data)
					page.LastModified = fileInfo.ModTime()

					pageMutex.Lock()
					pageCache[name] = page
					pageMutex.Unlock()

					return page, nil
				}
			}
		}

		page = &Page{}

		if FileExists(fn) {
			fileInfo, err := os.Stat(fn)
			if err != nil {
				log.WithError(err).Errorf("error getting page stats")
				return nil, err
			}
			page.LastModified = fileInfo.ModTime()

			data, err := os.ReadFile(fn)
			if err != nil {
				log.WithError(err).Errorf("error reading page %s", name)
				return nil, err
			}
			page.Content = string(data)
		} else {
			fn := filepath.Join(pagesDir, fmt.Sprintf("%s.md", name))
			data, err := builtinPages.ReadFile(fn)
			if err != nil {
				log.WithError(err).Errorf("error reading custom page %s", name)
				return nil, err
			}
			page.Content = string(data)
		}

		pageMutex.Lock()
		pageCache[name] = page
		pageMutex.Unlock()

		return page, nil
	}

	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		ctx := NewContext(s, r)

		page, err := getPage(name)
		if err != nil {
			if os.IsNotExist(err) {
				ctx.Error = true
				ctx.Message = s.tr(ctx, "PageNotFoundTitle")
				s.render("404", w, ctx)
				return
			}
			ctx.Error = true
			ctx.Message = s.tr(ctx, "ErrorPageError")
			s.render("message", w, ctx)
			return
		}

		markdownContent, err := RenderHTML(page.Content, ctx)
		if err != nil {
			log.WithError(err).Errorf("error rendering page %s", name)
			ctx.Error = true
			ctx.Message = s.tr(ctx, "ErrorRenderingPage")
			s.render("error", w, ctx)
			return
		}

		var frontmatter FrontMatter
		content, err := fmatter.Read([]byte(markdownContent), &frontmatter)
		if err != nil {
			log.WithError(err).Error("error parsing front matter")
			ctx.Error = true
			ctx.Message = s.tr(ctx, "ErrorLoadingPage")
			s.render("error", w, ctx)
			return
		}

		extensions := parser.CommonExtensions | parser.AutoHeadingIDs
		p := parser.NewWithExtensions(extensions)

		htmlFlags := html.CommonFlags
		opts := html.RendererOptions{
			Flags:     htmlFlags,
			Generator: "",
		}
		renderer := html.NewRenderer(opts)

		html := markdown.ToHTML(content, p, renderer)

		var title string

		if frontmatter.Title != "" {
			title = frontmatter.Title
		} else {
			title = strings.Title(name)
		}
		ctx.Title = title
		ctx.Meta.Description = frontmatter.Description

		ctx.Page = name
		ctx.Content = template.HTML(html)

		s.render("page", w, ctx)
	}
}
