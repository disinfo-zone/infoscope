// Save as: internal/feed/service.go
package feed

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"infoscope/internal/favicon"
)

type Service struct {
	db         *sql.DB
	logger     *log.Logger
	fetcher    *Fetcher
	faviconSvc *favicon.Service
	done       chan struct{}
}

func NewService(db *sql.DB, logger *log.Logger, faviconSvc *favicon.Service) *Service {
	s := &Service{
		db:         db,
		logger:     logger,
		faviconSvc: faviconSvc,
		done:       make(chan struct{}),
	}
	s.fetcher = NewFetcher(db, logger, faviconSvc)
	return s
}

func (s *Service) Start() {
	go s.updateLoop()
}

func (s *Service) Stop() {
	close(s.done)
}

func (s *Service) getUpdateInterval() time.Duration {
	// Get update interval from settings
	var intervalStr string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = 'update_interval'").Scan(&intervalStr)
	if err != nil {
		s.logger.Printf("Error getting update interval, using default: %v", err)
		return 15 * time.Minute
	}

	// Convert string to integer seconds
	interval, err := time.ParseDuration(intervalStr + "s")
	if err != nil {
		s.logger.Printf("Error parsing update interval, using default: %v", err)
		return 15 * time.Minute
	}

	// Ensure minimum interval
	if interval < time.Minute {
		interval = time.Minute
	}

	return interval
}

func (s *Service) updateLoop() {
	s.logger.Printf("Starting feed service update loop")

	// Do initial update
	if err := s.UpdateFeeds(context.Background()); err != nil {
		s.logger.Printf("Initial feed update failed: %v", err)
	}

	// Create ticker with initial interval
	interval := s.getUpdateInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.logger.Printf("Starting scheduled feed update")
			// Get current interval in case it was changed
			newInterval := s.getUpdateInterval()
			if newInterval != interval {
				s.logger.Printf("Update interval changed from %v to %v", interval, newInterval)
				ticker.Reset(newInterval)
				interval = newInterval
			}

			if err := s.UpdateFeeds(context.Background()); err != nil {
				s.logger.Printf("Scheduled feed update failed: %v", err)
			}

		case <-s.done:
			s.logger.Printf("Feed service shutting down")
			return
		}
	}
}

func (s *Service) UpdateFeeds(ctx context.Context) error {
	return s.fetcher.UpdateFeeds(ctx)
}

func (s *Service) AddFeed(url string) error {
	// Validate the feed first
	validationResult, err := ValidateFeedURL(url)
	if err != nil {
		return fmt.Errorf("feed validation failed: %w", err)
	}

	// Insert the feed
	result, err := s.db.Exec(
		"INSERT INTO feeds (url, title) VALUES (?, ?)",
		url, validationResult.Title,
	)
	if err != nil {
		return err
	}

	// Get the inserted feed ID
	feedID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	// Immediately fetch the feed
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	feedObj := Feed{
		ID:    feedID,
		URL:   url,
		Title: validationResult.Title,
	}

	fetchResult := s.fetcher.fetchFeed(ctx, feedObj)
	if fetchResult.Error != nil {
		s.logger.Printf("Error fetching new feed %s: %v", url, fetchResult.Error)
		return nil // Don't fail the add operation if initial fetch fails
	}

	return s.fetcher.saveFeedEntries(ctx, fetchResult)
}

func (s *Service) DeleteFeed(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete entries first
	_, err = tx.Exec("DELETE FROM entries WHERE feed_id = ?", id)
	if err != nil {
		return err
	}

	// Delete feed
	_, err = tx.Exec("DELETE FROM feeds WHERE id = ?", id)
	if err != nil {
		return err
	}

	return tx.Commit()
}
