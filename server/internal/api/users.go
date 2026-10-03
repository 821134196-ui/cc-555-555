package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"relocation/internal/models"
)

// UserView 用户及其当前座位（行政建单时选择员工用）
type UserView struct {
	models.User
	CurrentSeatID   *uint  `json:"current_seat_id"`
	CurrentSeatCode string `json:"current_seat_code"`
}

func (s *Server) listUsers(c *gin.Context) {
	role := c.Query("role")
	q := s.db.Order("id")
	if role != "" {
		q = q.Where("role = ?", role)
	}
	var users []models.User
	if err := q.Find(&users).Error; err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	var seats []models.Seat
	s.db.Where("occupant_id IS NOT NULL").Find(&seats)
	byOccupant := map[uint]models.Seat{}
	for _, seat := range seats {
		byOccupant[*seat.OccupantID] = seat
	}
	out := make([]UserView, 0, len(users))
	for _, u := range users {
		v := UserView{User: u}
		if seat, ok := byOccupant[u.ID]; ok {
			v.CurrentSeatID = &seat.ID
			v.CurrentSeatCode = seat.Code
		}
		out = append(out, v)
	}
	c.JSON(http.StatusOK, out)
}
