#!/bin/bash

# Test script to debug the checkbox save issue

echo "Testing Allow Public Theme Selection checkbox save functionality..."

# Get current setting from database
echo "Current database value:"
sqlite3 data/infoscope.db "SELECT key, value, type FROM settings WHERE key = 'allow_public_theme_selection';"

echo ""
echo "Test 1: Submit form with checkbox CHECKED (should save as true)"

# Create JSON payload with allowPublicThemeSelection: true
curl -X POST http://localhost:8080/admin/settings \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: test" \
  -d '{
    "siteTitle": "Test Site",
    "siteURL": "http://localhost:8080",
    "maxPosts": 100,
    "updateInterval": 3600,
    "allowPublicThemeSelection": true,
    "showBlogName": false,
    "showBodyText": false,
    "bodyTextLength": 200
  }' \
  --silent --show-error

echo ""
echo "Database value after Test 1:"
sqlite3 data/infoscope.db "SELECT key, value, type FROM settings WHERE key = 'allow_public_theme_selection';"

echo ""
echo "Test 2: Submit form with checkbox UNCHECKED (should save as false)"

# Create JSON payload with allowPublicThemeSelection: false
curl -X POST http://localhost:8080/admin/settings \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: test" \
  -d '{
    "siteTitle": "Test Site", 
    "siteURL": "http://localhost:8080",
    "maxPosts": 100,
    "updateInterval": 3600,
    "allowPublicThemeSelection": false,
    "showBlogName": false,
    "showBodyText": false,
    "bodyTextLength": 200
  }' \
  --silent --show-error

echo ""
echo "Database value after Test 2:"
sqlite3 data/infoscope.db "SELECT key, value, type FROM settings WHERE key = 'allow_public_theme_selection';"

echo ""
echo "Test complete. Check server logs for debug output."