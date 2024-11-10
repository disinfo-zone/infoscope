// internal/server/click_tracking.go
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type ClickStats struct {
	EntryID     int64  `json:"entryId"`
	Title       string `json:"title"`
	URL         string
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

	if !s.csrf.Validate(w, r) {
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
        SELECT COALESCE(SUM(click_count), 0) FROM clicks
    `).Scan(&stats.TotalClicks)
	if err != nil {
		stats.TotalClicks = 0
	}

	// Get top all time
	rows, err := s.db.Query(`
        SELECT e.id, e.title, e.url, c.click_count,
               strftime('%Y-%m-%d %H:%M:%S', c.last_clicked) as last_clicked
        FROM entries e
        INNER JOIN clicks c ON e.id = c.entry_id 
        ORDER BY c.click_count DESC, c.last_clicked DESC
        LIMIT 5
    `)
	if err != nil {
		return nil, fmt.Errorf("error getting top clicks: %w", err)
	}
	defer rows.Close()

	stats.TopAllTime = make([]ClickStats, 0)
	for rows.Next() {
		var stat ClickStats
		var lastClickedStr string
		if err := rows.Scan(&stat.EntryID, &stat.Title, &stat.URL, &stat.ClickCount, &lastClickedStr); err != nil {
			return nil, fmt.Errorf("error scanning click stats: %w", err)
		}
		lastClicked, err := time.ParseInLocation("2006-01-02 15:04:05", lastClickedStr, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("error parsing last clicked time: %w", err)
		}
		stat.LastClicked = lastClicked
		stats.TopAllTime = append(stats.TopAllTime, stat)
	}

	// Get weekly top with same timestamp format
	rows, err = s.db.Query(`
        SELECT e.id, e.title, e.url, c.click_count,
               strftime('%Y-%m-%d %H:%M:%S', c.last_clicked) as last_clicked
        FROM entries e
        INNER JOIN clicks c ON e.id = c.entry_id
        WHERE c.last_clicked >= datetime('now', '-7 days')
        ORDER BY c.click_count DESC, c.last_clicked DESC
        LIMIT 5
    `)
	if err != nil {
		return nil, fmt.Errorf("error getting weekly stats: %w", err)
	}
	defer rows.Close()

	stats.TopPastWeek = make([]ClickStats, 0)
	for rows.Next() {
		var stat ClickStats
		var lastClickedStr string
		if err := rows.Scan(&stat.EntryID, &stat.Title, &stat.URL, &stat.ClickCount, &lastClickedStr); err != nil {
			return nil, fmt.Errorf("error scanning weekly stats: %w", err)
		}
		lastClicked, err := time.ParseInLocation("2006-01-02 15:04:05", lastClickedStr, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("error parsing last clicked time: %w", err)
		}
		stat.LastClicked = lastClicked
		stats.TopPastWeek = append(stats.TopPastWeek, stat)
	}

	return stats, nil
}
