package blob

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// S3 stores blobs in a bucket of an S3-compatible service, addressed
// path-style (Endpoint/Bucket/key), signed with AWS Signature V4.
type S3 struct {
	Endpoint  string // e.g. http://minio.lan:9000 or https://s3.eu-west-1.amazonaws.com
	Bucket    string
	Region    string // default us-east-1
	AccessKey string
	SecretKey string
	Client    *http.Client
	now       func() time.Time
}

func (s *S3) Name() string { return "s3 " + strings.TrimRight(s.Endpoint, "/") + "/" + s.Bucket }

// EnsureBucket creates the bucket unless it exists.
func (s *S3) EnsureBucket(ctx context.Context) error {
	res, err := s.send(ctx, http.MethodPut, "", nil, 0, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusConflict { // BucketAlreadyOwnedByYou / BucketAlreadyExists
		return nil
	}
	return s.check(res, s.Bucket)
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	res, err := s.do(ctx, http.MethodPut, key, io.LimitReader(r, size), size, http.Header{"Content-Type": {contentType}})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return s.check(res, key)
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	res, err := s.do(ctx, http.MethodGet, key, nil, 0, nil)
	if err != nil {
		return nil, 0, err
	}
	if err := s.check(res, key); err != nil {
		res.Body.Close()
		return nil, 0, err
	}
	return res.Body, res.ContentLength, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	res, err := s.do(ctx, http.MethodDelete, key, nil, 0, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	return s.check(res, key)
}

func (s *S3) check(res *http.Response, key string) error {
	switch {
	case res.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case res.StatusCode >= 300:
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("blob: %s %s: %s: %s", res.Request.Method, key, res.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// do sends one signed request. Bodies are signed as UNSIGNED-PAYLOAD, so a
// stream need not be read twice; TLS protects it in transit.
func (s *S3) do(ctx context.Context, method, key string, body io.Reader, size int64, h http.Header) (*http.Response, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}
	return s.send(ctx, method, "/"+key, body, size, h)
}

// send signs and sends a request for the bucket plus path ("" = the bucket).
func (s *S3) send(ctx context.Context, method, path string, body io.Reader, size int64, h http.Header) (*http.Response, error) {
	u, err := url.Parse(strings.TrimRight(s.Endpoint, "/") + "/" + s.Bucket + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	for k, v := range h {
		req.Header[k] = v
	}
	if body != nil {
		req.ContentLength = size
	}
	s.sign(req)
	c := s.Client
	if c == nil {
		c = http.DefaultClient
	}
	return c.Do(req)
}

// sign adds AWS Signature V4 headers to req.
func (s *S3) sign(req *http.Request) {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	t := now().UTC()
	region := s.Region
	if region == "" {
		region = "us-east-1"
	}
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	const payload = "UNSIGNED-PAYLOAD"
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payload)
	req.Header.Set("Host", req.URL.Host)

	names := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if req.Header.Get("Content-Type") != "" {
		names = append([]string{"content-type"}, names...)
	}
	var canonHeaders strings.Builder
	for _, n := range names {
		v := req.Header.Get(n)
		if n == "host" {
			v = req.URL.Host
		}
		canonHeaders.WriteString(n + ":" + strings.TrimSpace(v) + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := strings.Join([]string{
		req.Method,
		escapePath(req.URL.EscapedPath()),
		req.URL.RawQuery,
		canonHeaders.String(),
		signed,
		payload,
	}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256(canonical)
	key := hmacSHA256([]byte("AWS4"+s.SecretKey), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.AccessKey+"/"+scope+
		", SignedHeaders="+signed+", Signature="+sig)
	req.Header.Del("Host") // net/http sends req.Host
}

// escapePath is the URI path as S3 signs it: each segment URI-encoded once.
// Keys are limited to [a-z0-9._/-], which need no escaping.
func escapePath(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

func hexSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}
