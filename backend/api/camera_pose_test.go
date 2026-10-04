package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"teslaxy/services"
)

func TestGetCameraPosesRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	SetupRoutes(router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/camera-poses", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d body %s", recorder.Code, recorder.Body.String())
	}

	var body services.PoseCatalog
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Frame.Name != "ground_nominal" || len(body.Cameras) != 6 {
		t.Fatalf("catalog %+v", body.Frame)
	}
	var front services.CameraPose
	for _, camera := range body.Cameras {
		if camera.Camera == "front" {
			front = camera
		}
	}
	if front.Camera != "front" || front.YawDeg != 0 || front.XM != 1.82 || front.ZM != 1.30 {
		t.Fatalf("front %+v", front)
	}
}
