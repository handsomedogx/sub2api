package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cyberevidence"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type evidenceHandlerCipher struct{}

func (evidenceHandlerCipher) Encrypt(s string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}
func (evidenceHandlerCipher) Decrypt(s string) (string, error) {
	raw, e := base64.StdEncoding.DecodeString(s)
	return string(raw), e
}

type evidenceHandlerStore struct{ r cyberevidence.Record }

func (s *evidenceHandlerStore) LoadConfig(context.Context) (cyberevidence.Config, error) {
	return cyberevidence.DefaultConfig(), nil
}
func (s *evidenceHandlerStore) SaveConfig(context.Context, cyberevidence.Config) error { return nil }
func (s *evidenceHandlerStore) Save(context.Context, int64, cyberevidence.Prepared, int) error {
	return nil
}
func (s *evidenceHandlerStore) Status(context.Context, int64, string) error { return nil }
func (s *evidenceHandlerStore) Get(context.Context, int64) (cyberevidence.Record, error) {
	return s.r, nil
}
func (s *evidenceHandlerStore) Delete(context.Context, int64) error {
	s.r.Status = "deleted"
	s.r.Ciphertext = ""
	return nil
}
func (s *evidenceHandlerStore) Cleanup(context.Context, int) (int64, error) { return 0, nil }
func TestCyberEvidencePreviewExcludesRetainedBodiesButExportIncludesThem(t *testing.T) {
	stored := cyberevidence.Payload{Input: `{"input":"preview"}`, RequestBody: `{"input":"FULL_REQUEST_TAIL"}`, RequestBodyBytes: 100, UpstreamResponse: `{"error":{"message":"FULL_REJECTION_TAIL"}}`}
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	ciphertext, err := (evidenceHandlerCipher{}).Encrypt(string(raw))
	require.NoError(t, err)
	store := &evidenceHandlerStore{r: cyberevidence.Record{LogID: 7, Status: "saved", Ciphertext: ciphertext}}
	es := cyberevidence.New(store, evidenceHandlerCipher{}, true)
	defer es.Close()
	cm := service.NewContentModerationService(nil, nil, nil, nil, nil, nil, nil, nil).WithCyberEvidence(es)
	h := NewContentModerationHandler(cm)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 17})
		c.Set(string(middleware.ContextKeyUserRole), "admin")
		c.Next()
	})
	router.GET("/logs/:id/evidence", h.GetCyberEvidence)
	router.GET("/logs/:id/evidence/export", h.ExportCyberEvidence)
	preview := httptest.NewRecorder()
	router.ServeHTTP(preview, httptest.NewRequest("GET", "/logs/7/evidence", nil))
	require.Equal(t, http.StatusOK, preview.Code)
	require.Contains(t, preview.Body.String(), "preview")
	require.NotContains(t, preview.Body.String(), "FULL_REQUEST_TAIL")
	require.NotContains(t, preview.Body.String(), "FULL_REJECTION_TAIL")
	export := httptest.NewRecorder()
	router.ServeHTTP(export, httptest.NewRequest("GET", "/logs/7/evidence/export", nil))
	require.Equal(t, http.StatusOK, export.Code)
	require.Contains(t, export.Body.String(), "FULL_REQUEST_TAIL")
	require.Contains(t, export.Body.String(), "FULL_REJECTION_TAIL")
	require.Contains(t, export.Header().Get("Content-Disposition"), "cyber-evidence-7.json")
}

func TestCyberEvidenceAdminOnlyReadExportDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &evidenceHandlerStore{r: cyberevidence.Record{LogID: 7, RequestID: "req_synthetic", Status: "historical_unavailable", OccurredAt: time.Now()}}
	es := cyberevidence.New(store, evidenceHandlerCipher{}, true)
	defer es.Close()
	cm := service.NewContentModerationService(nil, nil, nil, nil, nil, nil, nil, nil).WithCyberEvidence(es)
	h := NewContentModerationHandler(cm)
	for _, role := range []string{"", "user", "observer", "admin"} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			if role != "" {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 17})
				c.Set(string(middleware.ContextKeyUserRole), role)
			}
			c.Next()
		})
		router.GET("/logs/:id/evidence", h.GetCyberEvidence)
		router.GET("/logs/:id/evidence/export", h.ExportCyberEvidence)
		router.DELETE("/logs/:id/evidence", h.DeleteCyberEvidence)
		router.GET("/evidence/config", h.GetCyberEvidenceConfig)
		router.PUT("/evidence/config", h.UpdateCyberEvidenceConfig)
		for _, op := range []struct{ method, path string }{{"GET", "/logs/7/evidence"}, {"GET", "/logs/7/evidence/export"}, {"DELETE", "/logs/7/evidence"}, {"GET", "/evidence/config"}, {"PUT", "/evidence/config"}} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(op.method, op.path, nil))
			if role != "admin" {
				require.Equal(t, http.StatusForbidden, w.Code)
				require.NotContains(t, w.Body.String(), "req_synthetic")
			} else if op.method != "PUT" {
				require.Equal(t, http.StatusOK, w.Code)
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.NotContains(t, w.Body.String(), "ciphertext")
			}
		}
	}
}
