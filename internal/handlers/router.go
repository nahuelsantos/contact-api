// Package handlers holds the HTTP routes.
package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/forms"
	"github.com/nahuelsantos/contact-api/internal/guard"
	"github.com/nahuelsantos/contact-api/internal/submit"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

const (
	contentJSON = "application/json"
	contentForm = "application/x-www-form-urlencoded"
	serviceName = "contact-api"
	bytesPerKB  = 1024
)

// BuildInfo describes the running build.
type BuildInfo struct{ SHA string }

// v1Body is the v0 JSON response, kept for the existing websites.
type v1Body struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

const (
	v1Form        = "contact"
	v1MsgSent     = "Your message has been sent successfully! We will get back to you soon."
	v1MsgInvalid  = "Invalid request format"
	v1MsgFailed   = "Failed to send your message. Please try again later."
	v1MsgOrigin   = "Origin not allowed"
	v1MsgNotFound = "Unknown website"
	v1MsgRate     = "Too many requests. Please try again later."
)

// body is the v2 JSON response.
type body struct {
	OK     bool              `json:"ok"`
	Error  string            `json:"error,omitempty"`
	Fields forms.FieldErrors `json:"fields,omitempty"`
}

// NewRouter builds the HTTP router.
func NewRouter(svc *submit.Service, cfg *config.Config, info BuildInfo) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	log := svc.Log
	if log == nil {
		log = slog.Default()
	}

	r := gin.New()
	// requestLog sits outside Recovery so a recovered panic is still logged.
	r.Use(otelgin.Middleware(serviceName), requestLog(log), gin.Recovery())

	h := &v2{svc: svc, cfg: cfg}
	r.POST("/api/v2/sites/:site/forms/:form", h.submit)
	r.OPTIONS("/api/v2/sites/:site/forms/:form", h.preflight)
	r.POST("/api/v1/contact/:website", h.v1)
	r.OPTIONS("/api/v1/contact/:website", h.v1Preflight)

	healthz := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	version := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"sha": info.SHA}) }
	r.GET("/healthz", healthz)
	r.GET("/api/healthz", healthz)
	r.GET("/version.json", version)
	r.GET("/api/version.json", version)
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	return r
}

// requestLog logs method, route template, status and latency only.
func requestLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		log.LogAttrs(c.Request.Context(), slog.LevelInfo, "request",
			slog.String("method", c.Request.Method),
			slog.String("route", route),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency", time.Since(start)),
		)
	}
}

type v2 struct {
	svc *submit.Service
	cfg *config.Config
}

// cors adds CORS headers when the request origin is allowed for the site.
func (h *v2) cors(c *gin.Context, siteName, formName string, preflight bool) {
	site, _, ok := h.cfg.Lookup(siteName, formName)
	if !ok {
		return
	}
	c.Header("Vary", "Origin")
	origin := c.GetHeader("Origin")
	if origin == "" || !guard.OriginAllowed(origin, site.Origins) {
		return
	}
	c.Header("Access-Control-Allow-Origin", origin)
	if preflight {
		c.Header("Access-Control-Allow-Methods", http.MethodPost)
		c.Header("Access-Control-Allow-Headers", "Content-Type")
	}
}

func (h *v2) preflight(c *gin.Context) {
	h.cors(c, c.Param("site"), c.Param("form"), true)
	c.Status(http.StatusNoContent)
}

func (h *v2) v1Preflight(c *gin.Context) {
	h.cors(c, c.Param("website"), v1Form, true)
	c.Status(http.StatusNoContent)
}

func (h *v2) submit(c *gin.Context) {
	h.cors(c, c.Param("site"), c.Param("form"), false)

	mt, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mt != contentJSON && mt != contentForm {
		c.JSON(http.StatusUnsupportedMediaType, body{Error: "unsupported_media_type"})
		return
	}

	limit := int64(h.cfg.Server.MaxBodyKB) * bytesPerKB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	fields, err := readFields(c.Request.Body, mt)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		c.JSON(http.StatusRequestEntityTooLarge, body{Error: "too_large"})
		return
	}
	// A malformed body is treated as an empty submission, so lookup, origin
	// and rate checks still take precedence and validation reports the gaps.

	res := h.svc.Submit(c.Request.Context(), submit.Request{
		Site: c.Param("site"), Form: c.Param("form"),
		Origin: guard.RequestOrigin(c.Request), ClientIP: guard.ClientIP(c.Request),
		Fields: fields,
	})

	switch res.Outcome {
	case submit.Sent, submit.Spam:
		c.JSON(http.StatusOK, body{OK: true})
	case submit.Invalid:
		c.JSON(http.StatusUnprocessableEntity, body{Error: "invalid", Fields: res.Fields})
	case submit.Forbidden:
		c.JSON(http.StatusForbidden, body{Error: "forbidden"})
	case submit.NotFound:
		c.JSON(http.StatusNotFound, body{Error: "not_found"})
	case submit.RateLimited:
		secs := max(int(math.Ceil(res.RetryAfter.Seconds())), 1)
		c.Header("Retry-After", strconv.Itoa(secs))
		c.JSON(http.StatusTooManyRequests, body{Error: "rate_limited"})
	case submit.SendFailed:
		c.JSON(http.StatusBadGateway, body{Error: "send_failed"})
	}
}

// v1 serves the v0 endpoint as form "contact" of the named site. Like v0 it
// reads the body as JSON whatever the Content-Type, and answers CORS for the
// site's origins.
func (h *v2) v1(c *gin.Context) {
	h.cors(c, c.Param("website"), v1Form, false)
	limit := int64(h.cfg.Server.MaxBodyKB) * bytesPerKB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	fields, _ := readFields(c.Request.Body, contentJSON) // a bad or oversized body is an empty submission

	res := h.svc.Submit(c.Request.Context(), submit.Request{
		Site: c.Param("website"), Form: v1Form,
		Origin: guard.RequestOrigin(c.Request), ClientIP: guard.ClientIP(c.Request),
		Fields: fields,
	})

	switch res.Outcome {
	case submit.Sent, submit.Spam:
		c.JSON(http.StatusOK, v1Body{Success: true, Message: v1MsgSent})
	case submit.Invalid:
		c.JSON(http.StatusBadRequest, v1Body{Message: v1MsgInvalid})
	case submit.Forbidden:
		c.JSON(http.StatusForbidden, v1Body{Message: v1MsgOrigin})
	case submit.NotFound:
		c.JSON(http.StatusNotFound, v1Body{Message: v1MsgNotFound})
	case submit.RateLimited:
		secs := max(int(math.Ceil(res.RetryAfter.Seconds())), 1)
		c.Header("Retry-After", strconv.Itoa(secs))
		c.JSON(http.StatusTooManyRequests, v1Body{Message: v1MsgRate})
	case submit.SendFailed:
		c.JSON(http.StatusInternalServerError, v1Body{Message: v1MsgFailed})
	}
}

// readFields parses the request body into string fields. On a parse error it
// returns whatever was read (never nil) and the error.
func readFields(r io.Reader, mediaType string) (map[string]string, error) {
	out := map[string]string{}
	if mediaType == contentJSON {
		var raw map[string]any
		if err := json.NewDecoder(r).Decode(&raw); err != nil {
			return out, err //nolint:wrapcheck // caller inspects the error type
		}
		for k, v := range raw {
			switch t := v.(type) {
			case string:
				out[k] = t
			case bool:
				out[k] = strconv.FormatBool(t)
			case float64:
				out[k] = strconv.FormatFloat(t, 'f', -1, 64)
			case nil:
				out[k] = ""
			}
		}

		return out, nil
	}

	b, err := io.ReadAll(r)
	if err != nil {
		return out, err //nolint:wrapcheck // caller inspects the error type
	}
	q, _ := url.ParseQuery(string(b)) // keeps the well-formed pairs
	for k, vs := range q {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}

	return out, nil
}
