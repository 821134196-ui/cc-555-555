package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"relocation/internal/models"
)

// SeatView 座位及其在楼层图上的状态
type SeatView struct {
	models.Seat
	Status         string `json:"status"` // available | occupied | reserved
	OccupantName   string `json:"occupant_name,omitempty"`
	ActiveTicketID *uint  `json:"active_ticket_id,omitempty"`
	ActiveTicketNo string `json:"active_ticket_no,omitempty"`
}

func (s *Server) listFloors(c *gin.Context) {
	type row struct {
		Floor int
		Total int64
	}
	var rows []row
	if err := s.db.Model(&models.Seat{}).Select("floor, count(*) as total").
		Group("floor").Order("floor").Scan(&rows).Error; err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		seats := s.seatViews(r.Floor)
		var avail, occ, res int
		for _, sv := range seats {
			switch sv.Status {
			case "available":
				avail++
			case "occupied":
				occ++
			case "reserved":
				res++
			}
		}
		out = append(out, gin.H{
			"floor": r.Floor, "total": r.Total,
			"available": avail, "occupied": occ, "reserved": res,
		})
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) listFloorSeats(c *gin.Context) {
	floor, err := strconv.Atoi(c.Param("floor"))
	if err != nil {
		abort(c, http.StatusBadRequest, "楼层参数非法")
		return
	}
	c.JSON(http.StatusOK, s.seatViews(floor))
}

// seatViews 计算某层每个座位的状态：已占用 > 已预占 > 可用
func (s *Server) seatViews(floor int) []SeatView {
	var seats []models.Seat
	s.db.Preload("Occupant").Where("floor = ?", floor).
		Order("row, col").Find(&seats)

	// 进行中的搬迁单 → 目标座位即预占
	var active []models.Ticket
	s.db.Select("id", "ticket_no", "to_seat_id").
		Where("status IN ?", models.ActiveStatuses).Find(&active)
	reserved := map[uint]models.Ticket{}
	for _, t := range active {
		reserved[t.ToSeatID] = t
	}

	views := make([]SeatView, 0, len(seats))
	for _, seat := range seats {
		v := SeatView{Seat: seat, Status: "available"}
		if seat.Occupant != nil {
			v.Status = "occupied"
			v.OccupantName = seat.Occupant.Name
		} else if t, ok := reserved[seat.ID]; ok {
			v.Status = "reserved"
			v.ActiveTicketID = &t.ID
			v.ActiveTicketNo = t.TicketNo
		}
		views = append(views, v)
	}
	return views
}
