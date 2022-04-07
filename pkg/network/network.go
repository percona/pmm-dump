package network

import (
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
	"strings"
	"time"
)

func NewClient(httpC *fasthttp.Client) Client {
	return Client{
		client: httpC,
	}
}

type Client struct {
	client     *fasthttp.Client
	authCookie string
}

const AuthCookieName = "grafana_session"

func (c *Client) Do(req *fasthttp.Request) (*fasthttp.Response, error) {
	if len(c.authCookie) != 0 {
		req.Header.SetCookie(AuthCookieName, c.authCookie)
	}
	httpResp := fasthttp.AcquireResponse()
	err := c.client.Do(req, httpResp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make request in network client")
	}
	return httpResp, nil
}

// Copy-past from Client.Do(...)
func (c *Client) DoTimeout(req *fasthttp.Request, timeout time.Duration) (*fasthttp.Response, error) {
	if len(c.authCookie) != 0 {
		req.Header.SetCookie(AuthCookieName, c.authCookie)
	}
	httpResp := fasthttp.AcquireResponse()
	err := c.client.DoTimeout(req, httpResp, timeout)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make request in network client")
	}
	return httpResp, nil
}

func (c *Client) Post(url string) (int, []byte, error) {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.SetRequestURI(url)
	req.Header.SetMethod(fasthttp.MethodPost)
	httpResp, err := c.Do(req)
	defer fasthttp.ReleaseResponse(httpResp)
	return httpResp.StatusCode(), httpResp.Body(), err
}

func (c *Client) Get(url string) (int, []byte, error) {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.SetRequestURI(url)
	httpResp, err := c.Do(req)
	defer fasthttp.ReleaseResponse(httpResp)
	return httpResp.StatusCode(), httpResp.Body(), err
}

// Copy-past from Client.Get(...)
func (c *Client) GetTimeout(url string, timeout time.Duration) (int, []byte, error) {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.SetRequestURI(url)
	httpResp, err := c.DoTimeout(req, timeout)
	defer fasthttp.ReleaseResponse(httpResp)
	return httpResp.StatusCode(), httpResp.Body(), err
}

func (c *Client) Auth(pmmUrl, username, password string) error {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.SetRequestURI(fmt.Sprintf("%s/graph/login", pmmUrl))
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.SetContentType("application/json")
	//req.Header.SetContentType("application/json")
	ls := struct {
		Password string `json:"password"`
		User     string `json:"user"`
	}{password, username}
	lsb, err := json.Marshal(ls)
	if err != nil {
		return errors.Wrap(err, "failed to marshal login struct")
	}
	req.SetBody(lsb)
	//req.Header.SetCookie("grafana_session", c.authCookie)
	httpResp, err := c.Do(req)
	defer fasthttp.ReleaseResponse(httpResp)
	if err != nil {
		return errors.Wrap(err, "failed to make login request")
	}

	sessionRaw := httpResp.Header.PeekCookie(AuthCookieName)
	if len(sessionRaw) == 0 {
		return errors.New("authentication error")
	}

	c.authCookie = string(sessionRaw)
	c.authCookie = c.authCookie[strings.IndexRune(c.authCookie, '=')+1 : strings.IndexRune(c.authCookie, ';')]

	return nil
}
