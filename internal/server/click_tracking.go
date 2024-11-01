// internal/server/click_tracking.go
package server

import (
	"net/http"
	"strconv"
	"time"
)

type ClickStats struct {
	EntryID     int64     `json:"entryId"`
	Title       string    `json:"title"`
	ClickCount  int       `json:"clickCount"`
	LastClicked time.Time `json:"lastClicked"`
}

type DashboardStats struct {
	TotalClicks int64        `json:"totalClicks"`
	TopAllTime  []ClickStats `json:"topAllTime"`
	TopPastWeek []ClickStats `json:"topPastWeek"`
}

func (s *Server) handleClick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate CSRF token
	if !validateCSRFToken(s.csrfManager, w, r) {
		return
	}

	entryID := r.URL.Query().Get("id")
	if entryID == "" {
		http.Error(w, "Missing entry ID", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(entryID, 10, 64)
	if err != nil {
		http.Error(w, "Invalid entry ID", http.StatusBadRequest)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		s.logger.Printf("Error beginning transaction: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Update click count
	_, err = tx.Exec(`
        INSERT INTO clicks (entry_id, click_count, last_clicked)
        VALUES (?, 1, CURRENT_TIMESTAMP)
        ON CONFLICT(entry_id) DO UPDATE SET
        click_count = click_count + 1,
        last_clicked = CURRENT_TIMESTAMP
    `, id)
	if err != nil {
		s.logger.Printf("Error updating click count: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		s.logger.Printf("Error committing transaction: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) getClickStats() (*DashboardStats, error) {
	stats := &DashboardStats{}

	// Get total clicks
	err := s.db.QueryRow(`
        SELECT value FROM click_stats WHERE key = 'total_clicks'
    `).Scan(&stats.TotalClicks)
	if err != nil {
		return nil, err
	}

	// Get top 5 all time
	rows, err := s.db.Query(`
        SELECT e.id, e.title, c.click_count, DATETIME(c.last_clicked)
        FROM clicks c
        JOIN entries e ON e.id = c.entry_id
        ORDER BY c.click_count DESC, c.last_clicked DESC
        LIMIT 5
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats.TopAllTime = make([]ClickStats, 0)
	for rows.Next() {
		var stat ClickStats
		var lastClickedStr string
		if err := rows.Scan(&stat.EntryID, &stat.Title, &stat.ClickCount, &lastClickedStr); err != nil {
			return nil, err
		}
		lastClicked, err := time.Parse("2006-01-02 15:04:05", lastClickedStr)
		if err != nil {
			return nil, err
		}
		stat.LastClicked = lastClicked
		stats.TopAllTime = append(stats.TopAllTime, stat)
	}

	// Get top 5 past week
	rows, err = s.db.Query(`
        SELECT e.id, e.title, c.click_count, DATETIME(c.last_clicked)
        FROM clicks c
        JOIN entries e ON e.id = c.entry_id
        WHERE c.last_clicked > DATETIME('now', '-7 days')
        ORDER BY c.click_count DESC, c.last_clicked DESC
        LIMIT 5
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats.TopPastWeek = make([]ClickStats, 0)
	for rows.Next() {
		var stat ClickStats
		var lastClickedStr string
		if err := rows.Scan(&stat.EntryID, &stat.Title, &stat.ClickCount, &lastClickedStr); err != nil {
			return nil, err
		}
		lastClicked, err := time.Parse("2006-01-02 15:04:05", lastClickedStr)
		if err != nil {
			return nil, err
		}
		stat.LastClicked = lastClicked
		stats.TopPastWeek = append(stats.TopPastWeek, stat)
	}

	return stats, nil
}
