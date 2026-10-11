package subsonic

import (
	"context"
	"crypto/md5" // Subsonic's specified token protocol, not password storage.
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const PageSize = 100

type Song struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	CoverArt string  `json:"coverArt"`
	Duration float64 `json:"duration"`
	// Path is only used by the explicit local playback mode.
	Path string `json:"-"`
}
type Album struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Artist   string `json:"artist"`
	CoverArt string `json:"coverArt"`
	Songs    []Song `json:"song"`
}
type response struct {
	Status string `json:"status"`
	Error  struct {
		Code int `json:"code"`
	} `json:"error"`
	AlbumList struct {
		Albums []Album `json:"album"`
	} `json:"albumList2"`
	Album  Album `json:"album"`
	Search struct {
		Songs []Song `json:"song"`
	} `json:"searchResult3"`
}

type Client struct {
	base           url.URL
	user, password string
	http           *http.Client
}

func New(server, user, password string) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("请输入完整的 http(s) 服务器地址，不含认证、查询或片段")
	}
	if strings.TrimSpace(user) == "" {
		return nil, errors.New("请输入用户名")
	}
	// Keep TLS validation and reject cross-origin redirects carrying credentials.
	h := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host {
			return errors.New("服务器重定向被拒绝")
		}
		return nil
	}}
	return &Client{base: *u, user: user, password: password, http: h}, nil
}

func (c *Client) endpoint(method string, params url.Values) (string, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", errors.New("无法生成认证随机盐")
	}
	s := hex.EncodeToString(salt[:])
	token := md5.Sum([]byte(c.password + s))
	q := url.Values{"u": {c.user}, "s": {s}, "t": {hex.EncodeToString(token[:])}, "v": {"1.16.1"}, "c": {"godrome"}, "f": {"json"}}
	for k, v := range params {
		q[k] = append([]string(nil), v...)
	}
	u := c.base
	u.Path = strings.TrimRight(u.Path, "/") + "/rest/" + method + ".view"
	u.RawPath = ""
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *Client) get(ctx context.Context, method string, params url.Values) (*http.Response, error) {
	endpoint, err := c.endpoint(method, params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("无法创建请求")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// net/http errors include the authenticated URL. Never return them to UI/logs.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			return nil, errors.New("服务器请求超时")
		}
		return nil, errors.New("连接失败，请检查服务器地址、网络及 TLS 证书")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("服务器返回 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) call(ctx context.Context, method string, params url.Values) (response, error) {
	var wrapper struct {
		Response response `json:"subsonic-response"`
	}
	resp, err := c.get(ctx, method, params)
	if err != nil {
		return wrapper.Response, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil {
		return wrapper.Response, errors.New("读取服务器响应失败")
	}
	if len(data) > 4<<20 || json.Unmarshal(data, &wrapper) != nil {
		return wrapper.Response, errors.New("服务器响应不是有效的 Subsonic JSON")
	}
	r := wrapper.Response
	if r.Status != "ok" {
		if r.Error.Code == 40 || r.Error.Code == 41 {
			return r, errors.New("认证失败，请检查用户名和密码")
		}
		return r, fmt.Errorf("Subsonic 请求失败（代码 %d）", r.Error.Code)
	}
	return r, nil
}

func (c *Client) Ping(ctx context.Context) error { _, err := c.call(ctx, "ping", nil); return err }
func (c *Client) Albums(ctx context.Context, offset int) ([]Album, error) {
	r, err := c.call(ctx, "getAlbumList2", url.Values{"type": {"alphabeticalByName"}, "size": {strconv.Itoa(PageSize)}, "offset": {strconv.Itoa(max(0, offset))}})
	return r.AlbumList.Albums, err
}
func (c *Client) Album(ctx context.Context, id string) (Album, error) {
	r, err := c.call(ctx, "getAlbum", url.Values{"id": {id}})
	return r.Album, err
}
func (c *Client) Search(ctx context.Context, query string) ([]Song, error) {
	r, err := c.call(ctx, "search3", url.Values{"query": {query}, "songCount": {"100"}, "artistCount": {"0"}, "albumCount": {"0"}})
	return r.Search.Songs, err
}

// Songs returns one page of the library. An empty query is the OpenSubsonic way to list every song.
func (c *Client) Songs(ctx context.Context, offset int) ([]Song, error) {
	r, err := c.call(ctx, "search3", url.Values{
		"query": {`""`}, "songCount": {strconv.Itoa(PageSize)}, "songOffset": {strconv.Itoa(max(0, offset))},
		"artistCount": {"0"}, "albumCount": {"0"},
	})
	return r.Search.Songs, err
}
func (c *Client) StreamURL(id string) (string, error) {
	return c.endpoint("stream", url.Values{"id": {id}, "format": {"raw"}})
}
func (c *Client) Cover(ctx context.Context, id string) ([]byte, error) {
	resp, err := c.get(ctx, "getCoverArt", url.Values{"id": {id}, "size": {"256"}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("封面读取失败或超过 1 MiB")
	}
	return data, nil
}
