package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/urfave/cli/v2"

	"github.com/rs/zerolog/log"
)

type with struct {
	Do           bool
	Token        bool
	Config       bool
	Endpoint     bool
	EndpointFunc bool
	Client       bool
	RateLimiter  bool
	Flags        string
	Package      string
	Decoder      string
}

const (
	q = `// Code generated by "genwith {{.Flags}}"; DO NOT EDIT.

package {{.Package}}

import (
	"context"
	"encoding/xml"
	"encoding/json"
	"errors"
	"github.com/bzimmer/httpwares"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"time"
)

{{if .Client}}
type service struct {
	client *Client //nolint:golint,structcheck
}

// Option provides a configuration mechanism for a Client
type Option func(*Client) error

// NewClient creates a new client and applies all provided Options
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		client: &http.Client{},
	{{- if .Token}}
		token:  &oauth2.Token{},
	{{- end}}
	{{- if .Config}}
		config: oauth2.Config{
	{{- if .EndpointFunc}}
			Endpoint: Endpoint(),
	{{- end}}
	{{- if .Endpoint}}
			Endpoint: Endpoint,
	{{- end}}
		},
	{{- end}}
	}
	opts = append(opts, withServices())
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}
{{end}}

{{if .Config}}
// WithConfig sets the underlying oauth2.Config.
func WithConfig(config oauth2.Config) Option {
	return func(c *Client) error {
		c.config = config
		return nil
	}
}
// WithAPICredentials provides the client api credentials for the application.
func WithClientCredentials(clientID, clientSecret string) Option {
	return func(c *Client) error {
		c.config.ClientID = clientID
		c.config.ClientSecret = clientSecret
		return nil
	}
}

{{if or .Endpoint .EndpointFunc}}
// WithAutoRefresh refreshes access tokens automatically.
// The order of this option matters because it is dependent on the client's
// config and token. Use this option after With*Credentials.
func WithAutoRefresh(ctx context.Context) Option {
	return func(c *Client) error {
		c.client = c.config.Client(ctx, c.token)
		return nil
	}
}
{{end}}
{{end}}

{{if .Token}}
// WithToken sets the underlying oauth2.Token.
func WithToken(token *oauth2.Token) Option {
	return func(c *Client) error {
		c.token = token
		return nil
	}
}

// WithTokenCredentials provides the tokens for an authenticated user.
func WithTokenCredentials(accessToken, refreshToken string, expiry time.Time) Option {
	return func(c *Client) error {
		c.token.AccessToken = accessToken
		c.token.RefreshToken = refreshToken
		c.token.Expiry = expiry
		return nil
	}
}
{{end}}

{{if .RateLimiter}}
// WithRateLimiter rate limits the client's api calls
func WithRateLimiter(r *rate.Limiter) Option {
	return func(c *Client) error {
		if r == nil {
			return errors.New("nil limiter")
		}
		c.client.Transport = &httpwares.RateLimitTransport{
			Limiter:   r,
			Transport: c.client.Transport,
		}
		return nil
	}
}
{{end}}

// WithHTTPTracing enables tracing http calls.
func WithHTTPTracing(debug bool) Option {
	return func(c *Client) error {
		if !debug {
			return nil
		}
		c.client.Transport = &httpwares.VerboseTransport{
			Transport: c.client.Transport,
		}
		return nil
	}
}

// WithTransport sets the underlying http client transport.
func WithTransport(t http.RoundTripper) Option {
	return func(c *Client) error {
		if t == nil {
			return errors.New("nil transport")
		}
		c.client.Transport = t
		return nil
	}
}

// WithHTTPClient sets the underlying http client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) error {
		if client == nil {
			return errors.New("nil client")
		}
		c.client = client
		return nil
	}
}

{{if .Do}}
// do executes the http request and populates v with the result.
func (c *Client) do(req *http.Request, v interface{}) error {
	ctx := req.Context()
	res, err := c.client.Do(req)
	if err != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return err
		}
	}
	defer res.Body.Close()

	httpError := res.StatusCode >= http.StatusBadRequest

	var obj interface{}
	if httpError {
		obj = &Fault{}
	} else {
		obj = v
	}

	if obj != nil {
		err := {{.Decoder}}.NewDecoder(res.Body).Decode(obj)
		if err == io.EOF {
			err = nil // ignore EOF errors caused by empty response body
		}
		if httpError {
			switch q := obj.(type) {
			case *Fault:
				if q.Code == 0 {
					q.Code = res.StatusCode
				}
				if q.Message == "" {
					q.Message = http.StatusText(res.StatusCode)
				}
				return q
			case error:
				return q
			default:
				return q.(error)
			}
		}
		return err
	}

	return nil
}
{{end}}`
)

func format(ctx context.Context, file string) error {
	cmds := []*exec.Cmd{
		exec.CommandContext(ctx, "gofmt", "-w", "-s", file),
		exec.CommandContext(ctx, "goimports", "-w", file),
	}
	for _, cmd := range cmds {
		b, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Fprintln(os.Stderr, strings.TrimSpace(string(b)))
			return err
		}
	}
	return nil
}

func generate(w with, file, tmpl string) error {
	t, err := template.New("genwith").Parse(tmpl)
	if err != nil {
		log.Error().Err(err).Msg("parsing template")
		return err
	}
	src := new(bytes.Buffer)
	err = t.Execute(src, w)
	if err != nil {
		log.Error().Err(err).Msg("executing template")
		return err
	}
	return os.WriteFile(file, src.Bytes(), 0600)
}

func main() {
	app := &cli.App{
		Name:     "genwith",
		Usage:    "Generate new functional option clients",
		HelpName: "genwith",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "token",
				Value: false,
				Usage: "Include token-related options",
			},
			&cli.BoolFlag{
				Name:  "config",
				Value: false,
				Usage: "Include config-related options",
			},
			&cli.BoolFlag{
				Name:  "endpoint",
				Value: false,
				Usage: "Include oauth2.Endpoint var in config instantiation",
			},
			&cli.BoolFlag{
				Name:  "endpoint-func",
				Value: false,
				Usage: "Include oauth2.Endpoint func in config instantiation",
			},
			&cli.BoolFlag{
				Name:  "do",
				Value: false,
				Usage: "Include client.do function",
			},
			&cli.BoolFlag{
				Name:  "client",
				Value: false,
				Usage: "Include NewClient & options",
			},
			&cli.BoolFlag{
				Name:  "ratelimit",
				Value: false,
				Usage: "Include a rate limiting transport option",
			},
			&cli.StringFlag{
				Name:     "package",
				Value:    "",
				Required: true,
				Usage:    "The name of the package for generation",
			},
			&cli.StringFlag{
				Name:  "decoder",
				Value: "json",
				Usage: "The decoder to use",
			},
		},
		Before: func(c *cli.Context) error {
			if c.Bool("endpoint") && c.Bool("endpoint-func") {
				return errors.New("only one of --endpoint or --endpoint-func allowed")
			}
			if c.Bool("endpoint") || c.Bool("endpoint-func") {
				if !c.Bool("config") {
					return errors.New("--endpoint or --endpoint-func requires --config")
				}
			}
			return nil
		},
		ExitErrHandler: func(c *cli.Context, err error) {
			if err == nil {
				return
			}
			log.Error().Err(err).Msg(c.App.Name)
		},
		Action: func(c *cli.Context) error {
			w := with{
				Do:           c.Bool("do"),
				Token:        c.Bool("token"),
				Config:       c.Bool("config"),
				Endpoint:     c.Bool("endpoint"),
				EndpointFunc: c.Bool("endpoint-func"),
				Client:       c.Bool("client"),
				RateLimiter:  c.Bool("ratelimit"),
				Flags:        strings.Join(os.Args[1:], " "),
				Package:      c.String("package"),
				Decoder:      c.String("decoder")}
			file := fmt.Sprintf("%s_with.go", c.String("package"))
			if err := generate(w, file, q); err != nil {
				return err
			}
			return format(c.Context, file)
		},
	}
	if err := app.RunContext(context.Background(), os.Args); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
