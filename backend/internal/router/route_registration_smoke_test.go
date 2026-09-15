package router

import (
	"io"
	"log/slog"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/marketpal/marketpal/internal/handler"
	"github.com/marketpal/marketpal/internal/repository"
	"github.com/marketpal/marketpal/internal/service"
	"gorm.io/gorm"
)

// 验证商品路由注册不冲突（Gin 通配符/静态段共存），且编辑/上下架路由存在。
func TestProductRoutesRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var db *gorm.DB
	productRepo := repository.NewProductRepository(db)
	svc := service.NewProductService(productRepo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := handler.NewProductHandler(svc)
	api := r.Group("/api/v1")
	RegisterProductRoutes(api, h, "secret")
	found := map[string]bool{}
	for _, ri := range r.Routes() {
		found[ri.Method+" "+ri.Path] = true
	}
	for _, want := range []string{
		"POST /api/v1/products/:id/on-shelf",
		"POST /api/v1/products/:id/off-shelf",
		"PUT /api/v1/products/:id",
		"GET /api/v1/products/mine",
	} {
		if !found[want] {
			t.Fatalf("route missing: %s", want)
		}
	}
}
