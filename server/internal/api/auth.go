package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"relocation/internal/models"
)

const ctxUser = "currentUser"

// listLoginUsers 登录页可选用户（演示环境免登录）
func (s *Server) listLoginUsers(c *gin.Context) {
	var users []models.User
	if err := s.db.Select("id", "username", "name", "role").Order("id").Find(&users).Error; err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, users)
}

// login 演示登录：按用户名换取令牌（正式环境应换成账号密码 / SSO）
func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "username 必填")
		return
	}
	var u models.User
	if err := s.db.Where("username = ?", req.Username).First(&u).Error; err != nil {
		abort(c, http.StatusUnauthorized, "用户不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": u.Token, "user": u})
}

func (s *Server) me(c *gin.Context) {
	c.JSON(http.StatusOK, currentUser(c))
}

// authRequired 解析 Bearer 令牌并加载用户
func (s *Server) authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token := strings.TrimPrefix(h, "Bearer ")
		if token == "" || token == h {
			abort(c, http.StatusUnauthorized, "缺少或非法的 Authorization 头")
			return
		}
		var u models.User
		if err := s.db.Where("token = ?", token).First(&u).Error; err != nil {
			abort(c, http.StatusUnauthorized, "令牌无效")
			return
		}
		c.Set(ctxUser, &u)
		c.Next()
	}
}

// requireRole 角色守卫：行政不能代填 IT 测试结果，员工不能越权操作
func (s *Server) requireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		for _, r := range roles {
			if u.Role == r {
				c.Next()
				return
			}
		}
		abort(c, http.StatusForbidden, "当前角色无权执行此操作")
	}
}

func currentUser(c *gin.Context) *models.User {
	v, ok := c.Get(ctxUser)
	if !ok {
		return &models.User{}
	}
	return v.(*models.User)
}
