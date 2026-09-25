package submission

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func pageQueryContext(t *testing.T, query string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?"+query, nil)
	return context, recorder
}

func TestPageQueryRejectsPagesOutsideTheOffsetBudget(t *testing.T) {
	for _, raw := range []string{"0", "-1", "abc", "2147483648", "1000001", "99999999999999999999"} {
		t.Run(raw, func(t *testing.T) {
			context, recorder := pageQueryContext(t, "page="+raw)

			if _, _, ok := pageQuery(context); ok {
				t.Fatalf("pageQuery(page=%s) ok = true, want false", raw)
			}
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("pageQuery(page=%s) status = %d, want %d", raw, recorder.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestPageQueryAcceptsTheLargestAllowedPage(t *testing.T) {
	context, _ := pageQueryContext(t, "page=1000000&page_size=100")

	page, pageSize, ok := pageQuery(context)
	if !ok {
		t.Fatal("pageQuery(page=1000000, page_size=100) ok = false, want true")
	}
	if page != maxPage || pageSize != 100 {
		t.Fatalf("pageQuery = %d/%d, want %d/100", page, pageSize, maxPage)
	}
}
