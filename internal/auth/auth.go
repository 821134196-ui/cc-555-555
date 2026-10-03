package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"seatmove/internal/models"
)

type Claims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"usr"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
}

func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

func (m *Manager) Issue(u *models.User) (string, error) {
	claims := Claims{
		UserID:   u.ID,
		Username: u.Username,
		Name:     u.Name,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(12 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(m.secret)
}

func (m *Manager) parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

const ctxKey = "claims"

// Middleware 解析并校验 Bearer Token
func (m *Manager) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		tok := strings.TrimPrefix(h, "Bearer ")
		if tok == h || tok == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		claims, err := m.parse(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效"})
			return
		}
		c.Set(ctxKey, claims)
		c.Next()
	}
}

// RequireRole 要求当前用户属于指定角色之一，否则 403（行政不能代 IT 填测试结果等）
func RequireRole(roles ...string) gin.HandlerFunc {
	allow := map[string]bool{}
	for _, r := range roles {
		allow[r] = true
	}
	return func(c *gin.Context) {
		cl := Current(c)
		if cl == nil || !allow[cl.Role] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "当前角色无权执行该操作"})
			return
		}
		c.Next()
	}
}

func Current(c *gin.Context) *Claims {
	v, ok := c.Get(ctxKey)
	if !ok {
		return nil
	}
	cl, _ := v.(*Claims)
	return cl
}
