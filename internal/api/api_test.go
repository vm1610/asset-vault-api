package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"asset-vault-api/internal/auth"
	"asset-vault-api/internal/db"
	"asset-vault-api/internal/storage"
	"asset-vault-api/internal/worker"
)

const testKid = "api-test-key"

func init() {
	gin.SetMode(gin.TestMode)
}

type testEnv struct {
	router  http.Handler
	baseURL string
	priv    *rsa.PrivateKey
	pool    *worker.Pool
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating rsa key: %v", err)
	}
	n := base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(bigEndianExponent(priv.PublicKey.E))
	jwksSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{"kty": "RSA", "kid": testKid, "alg": "RS256", "use": "sig", "n": n, "e": e}},
		})
	}))
	t.Cleanup(jwksSrv.Close)

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	assets := db.NewAssetRepo(conn)
	jobs := db.NewJobRepo(conn)

	local, err := storage.NewLocalStorage(t.TempDir(), "http://api.local", "test-secret")
	if err != nil {
		t.Fatalf("creating local storage: %v", err)
	}

	processor := &worker.ThumbnailProcessor{Storage: local, Assets: assets, Jobs: jobs}
	pool := worker.NewPool(processor)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	pool.Start(ctx, 2)
	t.Cleanup(pool.Stop)

	keySet := auth.NewKeySet(jwksSrv.URL, time.Minute)

	router := NewRouter(Deps{
		Assets:        assets,
		Jobs:          jobs,
		Storage:       local,
		Pool:          pool,
		KeySet:        keySet,
		TempDir:       t.TempDir(),
		LocalStorage:  local,
		PresignExpiry: time.Minute,
	})

	return &testEnv{router: router, baseURL: "http://api.local", priv: priv, pool: pool}
}

func bigEndianExponent(v int) []byte {
	if v == 0 {
		return []byte{0}
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte(v & 0xff)}, b...)
		v >>= 8
	}
	return b
}

func (e *testEnv) token(t *testing.T, sub string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": sub,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = testKid
	signed, err := tok.SignedString(e.priv)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return signed
}

func (e *testEnv) do(t *testing.T, method, path, token string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func multipartFile(t *testing.T, fieldName, filename, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	part, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="` + fieldName + `"; filename="` + filename + `"`},
		"Content-Type":        {contentType},
	})
	if err != nil {
		t.Fatalf("creating multipart part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("writing multipart body: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}
	return buf, mw.FormDataContentType()
}

func testJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encoding test jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestAPI_UploadRequiresAuth(t *testing.T) {
	env := newTestEnv(t)
	body, ct := multipartFile(t, "file", "photo.jpg", "image/jpeg", testJPEGBytes(t))
	w := env.do(t, http.MethodPost, "/assets", "", body, ct)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", w.Code)
	}
}

func TestAPI_FullAssetLifecycle(t *testing.T) {
	env := newTestEnv(t)
	tok := env.token(t, "user-1")

	body, ct := multipartFile(t, "file", "photo.jpg", "image/jpeg", testJPEGBytes(t))
	w := env.do(t, http.MethodPost, "/assets", tok, body, ct)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on upload, got %d: %s", w.Code, w.Body.String())
	}
	var uploadResp struct {
		AssetID string `json:"asset_id"`
		JobID   string `json:"job_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("decoding upload response: %v", err)
	}

	var jobStatus string
	for i := 0; i < 50; i++ {
		w := env.do(t, http.MethodGet, "/jobs/"+uploadResp.JobID, tok, nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for job status, got %d", w.Code)
		}
		var jr map[string]any
		json.Unmarshal(w.Body.Bytes(), &jr)
		jobStatus = jr["status"].(string)
		if jobStatus == "completed" || jobStatus == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if jobStatus != "completed" {
		t.Fatalf("expected job to complete, last status: %s", jobStatus)
	}

	w = env.do(t, http.MethodGet, "/assets/"+uploadResp.AssetID, tok, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for get asset, got %d", w.Code)
	}
	var assetResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &assetResp)
	downloadURL, _ := assetResp["download_url"].(string)
	if downloadURL == "" {
		t.Fatal("expected download_url to be populated")
	}
	if _, ok := assetResp["thumbnail_url"]; !ok {
		t.Fatal("expected thumbnail_url once processing completed")
	}

	downloadReq := httptest.NewRequest(http.MethodGet, strippedPath(downloadURL), nil)
	dw := httptest.NewRecorder()
	env.router.ServeHTTP(dw, downloadReq)
	if dw.Code != http.StatusOK {
		t.Fatalf("expected 200 downloading via signed url, got %d", dw.Code)
	}
	if dw.Body.Len() == 0 {
		t.Fatal("expected non-empty downloaded content")
	}

	w = env.do(t, http.MethodGet, "/assets", tok, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for list, got %d", w.Code)
	}
	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	items := listResp["assets"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 asset in list, got %d", len(items))
	}

	otherTok := env.token(t, "user-2")
	w = env.do(t, http.MethodGet, "/assets/"+uploadResp.AssetID, otherTok, nil, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for another user's asset, got %d", w.Code)
	}

	w = env.do(t, http.MethodDelete, "/assets/"+uploadResp.AssetID, tok, nil, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete, got %d", w.Code)
	}
	w = env.do(t, http.MethodGet, "/assets/"+uploadResp.AssetID, tok, nil, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}

// strippedPath extracts the path+query from a full presigned URL so it can
// be replayed against the httptest router, which only sees request paths.
func strippedPath(fullURL string) string {
	const prefix = "http://api.local"
	if len(fullURL) > len(prefix) && fullURL[:len(prefix)] == prefix {
		return fullURL[len(prefix):]
	}
	return fullURL
}
