package db

import (
	"fmt"

	"gorm.io/gorm"

	"relocation/internal/models"
)

// Seed 写入演示数据：行政 / IT / 员工账号，3F 与 5F 两层座位。
// 已有数据时跳过，保证幂等。
func Seed(gdb *gorm.DB) error {
	var count int64
	if err := gdb.Model(&models.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	users := []models.User{
		{Username: "admin", Name: "王芳（行政）", Role: models.RoleAdmin, Token: "tok-admin"},
		{Username: "li.gong", Name: "李工（IT）", Role: models.RoleIT, Token: "tok-it1"},
		{Username: "zhao.gong", Name: "赵工（IT）", Role: models.RoleIT, Token: "tok-it2"},
	}
	employees := []struct{ username, name, token, ext string }{
		{"zhang.wei", "张伟", "tok-emp1", "6001"},
		{"li.na", "李娜", "tok-emp2", "6002"},
		{"wang.qiang", "王强", "tok-emp3", "6003"},
		{"liu.yang", "刘洋", "tok-emp4", "6004"},
		{"chen.jing", "陈静", "tok-emp5", "6005"},
		{"yang.fan", "杨帆", "tok-emp6", "6006"},
		{"huang.lei", "黄磊", "tok-emp7", "6007"},
		{"zhou.min", "周敏", "tok-emp8", "6008"},
	}
	empIDs := make([]uint, 0, len(employees))
	for _, e := range employees {
		users = append(users, models.User{
			Username: e.username, Name: e.name, Role: models.RoleEmployee,
			Extension: e.ext, Token: e.token,
		})
	}
	if err := gdb.Create(&users).Error; err != nil {
		return err
	}
	for _, u := range users {
		if u.Role == models.RoleEmployee {
			empIDs = append(empIDs, u.ID)
		}
	}

	// 座位：3F / 5F，A-E 排 × 1-6 列
	badPorts := map[string]bool{"5F-C03": true, "5F-B02": true} // 故障网口，用于演示 blocked 流程
	noPhone := map[string]bool{"5F-D05": true}
	noMonitor := map[string]bool{"5F-E06": true}

	var seats []models.Seat
	for _, floor := range []int{3, 5} {
		for row := 0; row < 5; row++ {
			for col := 1; col <= 6; col++ {
				code := fmt.Sprintf("%dF-%c%02d", floor, 'A'+row, col)
				s := models.Seat{
					Code: code, Floor: floor, Row: row, Col: col,
					PortCode:   "P" + code,
					PortOK:     !badPorts[code],
					HasPhone:   !noPhone[code],
					HasMonitor: !noMonitor[code],
				}
				if s.HasMonitor {
					s.AssetTag = "MON-" + code
				}
				seats = append(seats, s)
			}
		}
	}
	if err := gdb.Create(&seats).Error; err != nil {
		return err
	}

	// 员工初始就坐 3F（按座位顺序分配）
	idx := 0
	for i := range seats {
		if seats[i].Floor != 3 || idx >= len(empIDs) {
			continue
		}
		if err := gdb.Model(&models.Seat{}).Where("id = ?", seats[i].ID).
			Update("occupant_id", empIDs[idx]).Error; err != nil {
			return err
		}
		idx++
	}
	return nil
}
