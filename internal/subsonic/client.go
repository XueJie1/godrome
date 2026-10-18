package subsonic

import (
	"bytes"
	"context"
	"crypto/md5" // Subsonic's specified token protocol, not password storage.
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // Cover validation matches the UI's decoders.
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const PageSize = 100

var (
	ErrAuth    = errors.New("认证失败")
	ErrNetwork = errors.New("连接失败")
	ErrTimeout = errors.New("服务器请求超时")
)

// HTTPError is a non-200 response. Callers may use IsNetworkFailure to
// decide whether cached data can stand in for it.
type HTTPError struct{ Code int }

func (e *HTTPError) Error() string { return fmt.Sprintf("服务器返回 HTTP %d", e.Code) }

// IsNetworkFailure reports whether err means the server could not be
// reached or failed internally, as opposed to rejecting the credentials or
// the request. It is the only condition under which stale cache data is
// served.
func IsNetworkFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrNetwork) || errors.Is(err, ErrTimeout) {
		return true
	}
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.Code >= 500
}

// BlobCache is the disk cache the client uses for metadata responses and
// cover bytes. Keys are opaque to the cache.
type BlobCache interface {
	Get(key string) ([]byte, bool)
	Path(key string) (string, bool)
	Put(key string, data []byte) error
}

type Song struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	CoverArt string  `json:"coverArt"`
	AlbumID  string  `json:"albumId"`
	ArtistID string  `json:"artistId"`
	Starred  string  `json:"starred"`
	Duration float64 `json:"duration"`
	// Path is only used by the explicit local playback mode.
	Path string `json:"-"`
}

// Liked reports a server star. A non-empty starred timestamp is the Subsonic mark.
func (s Song) Liked() bool   { return s.Starred != "" }
func (a Album) Liked() bool  { return a.Starred != "" }
func (a Artist) Liked() bool { return a.Starred != "" }

type Album struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Artist   string `json:"artist"`
	ArtistID string `json:"artistId"`
	CoverArt string `json:"coverArt"`
	Starred  string `json:"starred"`
	Songs    []Song `json:"song"`
}

// Artist is an ID3 artist returned by getStarred2. Name is the display name.
type Artist struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CoverArt string `json:"coverArt"`
	Starred  string `json:"starred"`
}

// Starred is the ID3 starred collection from getStarred2.
type Starred struct {
	Artists []Artist `json:"artist"`
	Albums  []Album  `json:"album"`
	Songs   []Song   `json:"song"`
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
	Starred Starred `json:"starred2"`
}

type Client struct {
	base           url.URL
	user, password string
	http           *http.Client

	cache BlobCache
	ns    string
	// offline gates serving cached metadata after a network failure.
	offline atomic.Bool
	// stale records whether the last successful call came from the cache.
	stale atomic.Bool
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

// UseCache attaches a disk cache. Keys are namespaced by server and user so
// two accounts never read each other's library or starred metadata.
func (c *Client) UseCache(cache BlobCache, offline bool) {
	c.cache = cache
	c.offline.Store(offline)
	if cache != nil {
		c.ns = cacheNamespace(c.base.String(), c.user)
	}
}

func (c *Client) SetOfflineCache(on bool) { c.offline.Store(on) }

// Stale reports whether the most recent successful metadata call was
// answered from the cache after a network failure.
func (c *Client) Stale() bool { return c.stale.Load() }

// OfflineAvailable reports whether the first album page is cached, which is
// what the app needs to open in offline mode.
func (c *Client) OfflineAvailable() bool {
	params := url.Values{
		"type":   {"alphabeticalByName"},
		"size":   {strconv.Itoa(PageSize)},
		"offset": {"0"},
	}
	data, ok := c.cached("getAlbumList2", params)
	if !ok {
		return false
	}
	var wrapper struct {
		Response response `json:"subsonic-response"`
	}
	return json.Unmarshal(data, &wrapper) == nil && wrapper.Response.Status == "ok"
}

func cacheNamespace(server, user string) string {
	sum := sha256.Sum256([]byte(server))
	return hex.EncodeToString(sum[:8]) + "|" + user
}

// cacheable limits the cache to read-only library calls. ping, star, and
// unstar must always reach the server.
func cacheable(method string) bool {
	switch method {
	case "getAlbumList2", "getAlbum", "search3", "getStarred2":
		return true
	}
	return false
}

func (c *Client) cacheKey(method string, params url.Values) string {
	return "meta|" + c.ns + "|" + method + "|" + params.Encode()
}

func (c *Client) coverKey(id string) string {
	return "cover|" + c.ns + "|" + id + "|256"
}

func (c *Client) cached(method string, params url.Values) ([]byte, bool) {
	if c.cache == nil || !cacheable(method) {
		return nil, false
	}
	return c.cache.Get(c.cacheKey(method, params))
}

func (c *Client) remember(method string, params url.Values, data []byte) {
	if c.cache == nil || !cacheable(method) {
		return
	}
	_ = c.cache.Put(c.cacheKey(method, params), data)
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
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w，请检查服务器地址、网络及 TLS 证书", ErrNetwork)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &HTTPError{Code: resp.StatusCode}
	}
	return resp, nil
}

func (c *Client) call(ctx context.Context, method string, params url.Values) (response, error) {
	var wrapper struct {
		Response response `json:"subsonic-response"`
	}
	resp, err := c.get(ctx, method, params)
	if err != nil {
		if c.offline.Load() && IsNetworkFailure(err) {
			if cached, ok := c.cached(method, params); ok &&
				json.Unmarshal(cached, &wrapper) == nil && wrapper.Response.Status == "ok" {
				c.stale.Store(true)
				return wrapper.Response, nil
			}
		}
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
			return r, fmt.Errorf("%w，请检查用户名和密码", ErrAuth)
		}
		return r, fmt.Errorf("Subsonic 请求失败（代码 %d）", r.Error.Code)
	}
	c.stale.Store(false)
	c.remember(method, params, data)
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

// Starred returns the current user's starred songs, albums, and artists, organized by ID3 tags.
func (c *Client) Starred(ctx context.Context) (Starred, error) {
	r, err := c.call(ctx, "getStarred2", nil)
	return r.Starred, err
}

// Star marks a song, album, or artist. At least one id must be set. Song ids use id, album ids use albumId, and artist ids use artistId.
func (c *Client) Star(ctx context.Context, songID, albumID, artistID string) error {
	_, err := c.call(ctx, "star", starParams(songID, albumID, artistID))
	return err
}

// Unstar removes a star. The parameters match Star.
func (c *Client) Unstar(ctx context.Context, songID, albumID, artistID string) error {
	_, err := c.call(ctx, "unstar", starParams(songID, albumID, artistID))
	return err
}

func starParams(songID, albumID, artistID string) url.Values {
	q := url.Values{}
	if songID != "" {
		q.Set("id", songID)
	}
	if albumID != "" {
		q.Set("albumId", albumID)
	}
	if artistID != "" {
		q.Set("artistId", artistID)
	}
	return q
}

func (c *Client) StreamURL(id string) (string, error) {
	return c.endpoint("stream", url.Values{"id": {id}, "format": {"raw"}})
}
func (c *Client) Cover(ctx context.Context, id string) ([]byte, error) {
	key := c.coverKey(id)
	if c.cache != nil {
		if data, ok := c.cache.Get(key); ok && validImage(data) {
			return data, nil
		}
	}
	resp, err := c.get(ctx, "getCoverArt", url.Values{"id": {id}, "size": {"256"}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("封面读取失败或超过 1 MiB")
	}
	if !validImage(data) {
		return nil, errors.New("封面不是有效的 JPEG 或 PNG 图片")
	}
	if c.cache != nil {
		_ = c.cache.Put(key, data)
	}
	return data, nil
}

// CoverPath returns a file:// suitable path for the cover, downloading it
// first when needed. The cache normally provides that file; without a
// cache it falls back to a stable temporary file so media widgets still get
// artwork.
func (c *Client) CoverPath(ctx context.Context, id string) (string, error) {
	data, err := c.Cover(ctx, id)
	if err != nil {
		return "", err
	}
	if c.cache != nil {
		if path, ok := c.cache.Path(c.coverKey(id)); ok {
			return path, nil
		}
	}
	sum := sha256.Sum256([]byte(id))
	name := "godrome-cover-" + hex.EncodeToString(sum[:8]) + coverExtension(data)
	path := filepath.Join(os.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// validImage matches the UI's decode limits: JPEG or PNG, at most 1024×1024
// before it is scaled to the 256-pixel thumbnail.
func validImage(data []byte) bool {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && cfg.Width >= 1 && cfg.Height >= 1 && cfg.Width <= 1024 && cfg.Height <= 1024
}

func coverExtension(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	default:
		return ".img"
	}
}
