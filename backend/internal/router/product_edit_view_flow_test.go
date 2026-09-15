package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/marketpal/marketpal/internal/handler"
	"github.com/marketpal/marketpal/internal/middleware"
	"github.com/marketpal/marketpal/internal/model"
	"github.com/marketpal/marketpal/internal/repository"
	"github.com/marketpal/marketpal/internal/service"
	"github.com/marketpal/marketpal/internal/util"
	"gorm.io/gorm"
)

// 页面编辑流程的 HTTP 级测试：真实 Gin 路由 + 真实 service/repository（内存 SQLite 隔离数据），
// 模拟编辑页发出的请求序列，验证进入编辑/保存返回/取消返回浏览量均不变，非卖家仍被拦截。

const editFlowSecret = "edit-flow-test-secret"

type editFlowEnv struct {
	engine      *gin.Engine
	sellerToken string
	otherToken  string
	productID   uint
}

// newEditFlowEnv 构造隔离的测试环境：独立内存库、卖家/买家两个账号、一件在售商品（已有 5 次浏览）。
func newEditFlowEnv(t *testing.T) *editFlowEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&model.User{}, &model.Product{}, &model.Favorite{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	seller := &model.User{Username: "seller", PasswordHash: "x", Nickname: "卖家", Role: "user"}
	buyer := &model.User{Username: "buyer", PasswordHash: "x", Nickname: "买家", Role: "user"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("seed seller: %v", err)
	}
	if err := db.Create(buyer).Error; err != nil {
		t.Fatalf("seed buyer: %v", err)
	}
	product := &model.Product{
		SellerID: seller.ID, Title: "iPhone 13", Description: "九成新，配件齐全",
		OriginalPrice: 5999, Price: 3999, Condition: "almost_new", Category: "digital",
		Images: "/uploads/a.jpg,/uploads/b.jpg", Status: "on_sale", ViewCount: 5, FavoriteCount: 2,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	productRepo := repository.NewProductRepository(db)
	favoriteRepo := repository.NewFavoriteRepository(db)
	svc := service.NewProductService(productRepo, favoriteRepo, logger)
	h := handler.NewProductHandler(svc)

	r := gin.New()
	r.Use(middleware.ErrorHandler(logger))
	api := r.Group("/api/v1")
	RegisterProductRoutes(api, h, editFlowSecret)

	mint := func(u *model.User) string {
		token, err := util.GenerateToken(editFlowSecret, u.ID, u.Username, u.Role, time.Hour)
		if err != nil {
			t.Fatalf("mint token: %v", err)
		}
		return token
	}
	return &editFlowEnv{
		engine:      r,
		sellerToken: mint(seller),
		otherToken:  mint(buyer),
		productID:   product.ID,
	}
}

// do 发送 HTTP 请求并解析统一响应体。
func (e *editFlowEnv) do(t *testing.T, method, path, token string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %s %s: %v (body=%s)", method, path, err, w.Body.String())
	}
	return w.Code, resp
}

// viewCount 通过编辑回填接口（不计数）读取当前浏览量。
func (e *editFlowEnv) viewCount(t *testing.T) float64 {
	t.Helper()
	status, resp := e.do(t, http.MethodGet, fmt.Sprintf("/api/v1/products/%d/edit", e.productID), e.sellerToken, nil)
	if status != http.StatusOK {
		t.Fatalf("read view count failed: status=%d resp=%v", status, resp)
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing data in response: %v", resp)
	}
	return data["view_count"].(float64)
}

func productPath(id uint) string { return fmt.Sprintf("/api/v1/products/%d", id) }

// TestProductEditPageViewCountFlow 模拟编辑页“进入 → 保存返回 → 取消返回”完整流程。
func TestProductEditPageViewCountFlow(t *testing.T) {
	env := newEditFlowEnv(t)
	const baseline = 5.0

	// 前置校验：他人真实浏览详情，浏览量正常 +1（计数功能本身可用）。
	status, _ := env.do(t, http.MethodGet, productPath(env.productID), env.otherToken, nil)
	if status != http.StatusOK {
		t.Fatalf("buyer view detail: status=%d", status)
	}
	if got := env.viewCount(t); got != baseline+1 {
		t.Fatalf("buyer view should incr count, want %v got %v", baseline+1, got)
	}
	// 将浏览量恢复为基准值语义：后续断言以当前值 6 为基准。
	const afterBuyerView = baseline + 1

	// 1) 进入编辑页（连续 3 次）：回填完整且浏览量不变。
	for i := 0; i < 3; i++ {
		status, resp := env.do(t, http.MethodGet, productPath(env.productID)+"/edit", env.sellerToken, nil)
		if status != http.StatusOK {
			t.Fatalf("enter edit page #%d: status=%d resp=%v", i, status, resp)
		}
		data := resp["data"].(map[string]interface{})
		if data["title"] != "iPhone 13" || data["description"] == "" {
			t.Fatalf("edit info incomplete: %v", data)
		}
		images, _ := data["images"].([]interface{})
		if len(images) != 2 {
			t.Fatalf("edit images incomplete: %v", data["images"])
		}
		if data["view_count"].(float64) != afterBuyerView {
			t.Fatalf("enter edit page #%d changed view count: %v", i, data["view_count"])
		}
	}
	if got := env.viewCount(t); got != afterBuyerView {
		t.Fatalf("after entering edit page 3 times, want %v got %v", afterBuyerView, got)
	}

	// 2) 保存后返回详情：浏览量不变，且新内容已生效。
	status, resp := env.do(t, http.MethodPut, productPath(env.productID), env.sellerToken, map[string]interface{}{
		"title": "iPhone 13 Pro Max", "price": 4599,
	})
	if status != http.StatusOK {
		t.Fatalf("save edit: status=%d resp=%v", status, resp)
	}
	status, resp = env.do(t, http.MethodGet, productPath(env.productID), env.sellerToken, nil)
	if status != http.StatusOK {
		t.Fatalf("return to detail after save: status=%d", status)
	}
	data := resp["data"].(map[string]interface{})
	if data["title"] != "iPhone 13 Pro Max" {
		t.Fatalf("saved title not applied: %v", data["title"])
	}
	if data["view_count"].(float64) != afterBuyerView {
		t.Fatalf("save-and-return changed view count: want %v got %v", afterBuyerView, data["view_count"])
	}

	// 3) 取消后返回详情：浏览量不变。
	status, resp = env.do(t, http.MethodGet, productPath(env.productID), env.sellerToken, nil)
	if status != http.StatusOK {
		t.Fatalf("return to detail after cancel: status=%d", status)
	}
	if got := resp["data"].(map[string]interface{})["view_count"].(float64); got != afterBuyerView {
		t.Fatalf("cancel-and-return changed view count: want %v got %v", afterBuyerView, got)
	}
	if got := env.viewCount(t); got != afterBuyerView {
		t.Fatalf("final view count: want %v got %v", afterBuyerView, got)
	}
	// 收藏量在整条链路中同样保持不变。
	_, resp = env.do(t, http.MethodGet, productPath(env.productID)+"/edit", env.sellerToken, nil)
	if got := resp["data"].(map[string]interface{})["favorite_count"].(float64); got != 2 {
		t.Fatalf("favorite count must stay 2, got %v", got)
	}
}

// TestProductEditPageNonSellerBlocked 非卖家访问编辑回填接口被拦截，匿名访问被拦截。
func TestProductEditPageNonSellerBlocked(t *testing.T) {
	env := newEditFlowEnv(t)

	status, resp := env.do(t, http.MethodGet, productPath(env.productID)+"/edit", env.otherToken, nil)
	if status != http.StatusForbidden {
		t.Fatalf("non-seller edit fetch: want 403 got %d resp=%v", status, resp)
	}
	if resp["code"].(float64) != 40300 {
		t.Fatalf("non-seller edit fetch: want code 40300 got %v", resp["code"])
	}

	status, _ = env.do(t, http.MethodGet, productPath(env.productID)+"/edit", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous edit fetch: want 401 got %d", status)
	}

	// 拦截不产生浏览量。
	if got := env.viewCount(t); got != 5 {
		t.Fatalf("blocked fetches must not count views, got %v", got)
	}
}
