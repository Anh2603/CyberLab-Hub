package webapp

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed templates/*.html static/*
var content embed.FS

var pages = template.Must(template.ParseFS(content, "templates/*.html"))

var assets = func() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return sub
}()

type pageData struct {
	Title string
	Mode  string
}

func Register(router *gin.Engine) {
	web := router.Group("")
	web.Use(securityHeaders())
	web.StaticFS("/assets", http.FS(assets))
	web.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/login") })
	web.GET("/login", func(c *gin.Context) {
		render(c, "auth.html", pageData{Title: "Log in", Mode: "login"})
	})
	web.GET("/signup", func(c *gin.Context) {
		render(c, "auth.html", pageData{Title: "Create account", Mode: "signup"})
	})
	web.GET("/app", func(c *gin.Context) {
		render(c, "app.html", pageData{Title: "Workspace"})
	})
}

func render(c *gin.Context, name string, data pageData) {
	var output bytes.Buffer
	if err := pages.ExecuteTemplate(&output, name, data); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", output.Bytes())
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'self'; base-uri 'none'; connect-src 'self' ws: wss:; font-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Next()
	}
}
