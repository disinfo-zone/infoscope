# Infoscope

Infoscope is a minimalist public RSS river reader designed to reduce information anxiety while helping users discover interesting content. It reimagines how we consume online information by treating content as a flowing river rather than an accumulating inbox.

## The Problem

Traditional RSS readers can create anxiety:
- Unread counts become a constant reminder of "falling behind"
- The pressure to "catch up" turns reading into a chore 
- Private feed lists keep valuable curation hidden from others
- Infinite archives make it hard to "let go" of old content

## The Solution

Infoscope approaches these challenges differently. Instead of treating content as something to be collected and archived, Infoscope presents it as a flowing river. Like a real river, you can stop by whenever you want to see what's flowing past and take what interests you in the moment. Let the rest flow by without worry and return tomorrow to find fresh content.

### Public Curation
Most RSS readers are private, treating feed selection as a personal choice. Infoscope takes a different approach. Feed selection becomes a form of public curation and curators can share their expertise through feed choices.

### Privacy-First Design
While curation is public, reader privacy is paramount. Infoscope has no required user accounts, no personal data collection, and minimal cookie usage (only for admin authentication and CSRF protection). The only statistics collected (clicks on links) are fully anonymous with no user data recorded.

### Security Features
- CSRF protection for all forms and API endpoints
- Secure session handling for admin access
- SQLite database with proper SQL injection prevention
- Configurable production mode with enhanced security

### Minimalist Interface
The interface is intentionally simple in keeping with the guiding ethos. It is a clean, distraction-free retro design with a focus on content discovery. This means:
- No infinite scroll (admins define the number of links shown)
- No pagination
- No unread counts
- No retention of old entries
- Customizable header/footer links and images

## Screenshots

### Desktop Views
<div style="display: flex; flex-wrap: wrap; gap: 10px;">
    <img src="https://images.disinfo.zone/uploads/uwclLqRY5NXaf2hlqQICUlYyDP5ieGsDJRsbXshF.jpg" alt="Desktop Homepage" width="600px"/>
    <img src="https://images.disinfo.zone/uploads/AC5iiYk1kiYJJv2e9pAsFgAxPLZzJXD2hmVxs0gw.jpg" alt="Dashboard" width="600px"/>
    <img src="https://images.disinfo.zone/uploads/OYJZLkh5eoLb1VM1XLzaPLVIT36Hi02OFgxMMOXE.jpg" alt="Feed Management" width="600px"/>
</div>

### Mobile Views
<div style="display: flex; flex-wrap: wrap; gap: 10px;">
    <img src="https://images.disinfo.zone/uploads/7Hs0R2XpFxoCla1rNidfp45W5cprCtG8FtQFAvNH.png" alt="Mobile Homepage" width="300px"/>
    <img src="https://images.disinfo.zone/uploads/JSqsLfN9QMy6AUEpqMfa83iRAHIzbT6L1K8s7GRI.png" alt="Mobile Dashboard" width="300px"/>
    <img src="https://images.disinfo.zone/uploads/qaq39EnCisnCMulDW8ukdJXlheQJ8zUD9fxl8R1v.png" alt="Mobile Settings" width="300px"/>
</div>

## Installation

### Quick Start

Download Linux and Windows AMD64 binaries from the releases page or build from scratch:

```bash
# Download and build
git clone https://github.com/disinfo-zone/infoscope.git
cd infoscope
go build ./cmd/infoscope
```
To run on linux:
```
# Run in development mode
./infoscope
```
```
# Run in production mode
./infoscope -prod
```
On windows:
```
# Run in development mode
infoscope.exe
```
```
# Run in production mode
infoscope.exe -prod
```

Visit `http://localhost:8080/setup` to complete installation. It should default to this page until an admin account has been created. Once created, admin page is accessed at `/admin`.

### Configuration

Command line flags:
- `-port`: HTTP port (default: 8080)
- `-db`: Database path (default: data/infoscope.db)
- `-data`: Data directory path (default: data)
- `-version`: Print version information
- `-prod`: Enable production mode with enhanced security
- `-no-template-updates`: Disable automatic template updates (for example if you edit the html)

Environment variables:
- `INFOSCOPE_PORT`: HTTP port
- `INFOSCOPE_DB_PATH`: Database path
- `INFOSCOPE_DATA_PATH`: Data directory path

## Docker Installation

Run Infoscope in production mode using Docker:

```bash
docker run -d \
  --name infoscope \
  -p 8080:8080 \
  -v infoscope-data:/app/data \
  -v infoscope-web:/app/web \
  -e INFOSCOPE_PRODUCTION=true \
  ghcr.io/disinfo-zone/infoscope:latest
```
### Environment variables supported in Docker:

- `INFOSCOPE_PORT`: HTTP port (default: 8080)
- `INFOSCOPE_DB_PATH`: Database path (default: /app/data/infoscope.db)
- `INFOSCOPE_DATA_PATH`: Data directory (default: /app/data)
- `INFOSCOPE_WEB_PATH`: Web content path (default: /app/web)
- `INFOSCOPE_PRODUCTION`: Enable production mode (true/false)
- `INFOSCOPE_NO_TEMPLATE_UPDATES`: Disable template updates (true/false)

### Volumes:

- `/app/data`: Database and data files
- `/app/web`: Web content and templates

### Directory Permissions for Docker

**Important for Rootless Containers & Upgraders:**

When running Infoscope via Docker, especially with rootless containers (as this project uses) or if you've configured Docker to use a specific non-root user, ensure the process inside the container has **read and write permissions** for the host directories mapped to the following volumes:
*   `/app/data`: Essential for database operations.
*   `/app/web`: Required if automatic template updates are enabled (i.e., not using the `-no-template-updates` flag or `INFOSCOPE_NO_TEMPLATE_UPDATES=true` environment variable).

**Why is this important?**
- Incorrect permissions can prevent Infoscope from starting, writing to its database, or managing web templates.
- Users upgrading from older versions (which might have run as root in the container by default) should particularly verify these permissions to avoid disruptions.

**How to ensure correct permissions:**
- Adjust the ownership of your host directories to match the UID (User ID) and GID (Group ID) of the user that the Infoscope process runs as inside your container. This depends on your specific Docker image and any user configurations (e.g., via `docker run --user ...`).
- Alternatively, set directory permissions on the host (e.g., `chmod 775` for the host paths), ensuring the container's user can write to them.

For example, if your Docker volume on the host is `/my/infoscope/data` and it's mapped to `/app/data` in the container:
```bash
# Determine the UID/GID of the user running Infoscope in your container.
# Common examples include 1000:1000 (default user) or 65532:65532 (often 'nobody').
# Replace YOUR_CONTAINER_UID and YOUR_CONTAINER_GID with the correct values.

# Example: Change ownership to UID 65532, GID 65532
# sudo chown -R 65532:65532 /my/infoscope/data
# sudo chown -R 65532:65532 /my/infoscope/web

# Example: Change ownership to UID 1000, GID 1000
# sudo chown -R 1000:1000 /my/infoscope/data
# sudo chown -R 1000:1000 /my/infoscope/web

# Or, more open permissions (use with caution):
# sudo chmod -R 777 /my/infoscope/data
```
Consult your Docker and system documentation for the best way to manage permissions for your specific setup. You may need to inspect your Docker image or use commands like `docker exec <container_name_or_id> id` to find the UID/GID of the running process.

## Additional Setup Notes

### Template Management

By default, Infoscope automatically extracts and updates its web templates and static files on startup to ensure you're always running the latest version. This behavior can be disabled with the `-no-template-updates` flag, which is useful for:

- Production environments where templates shouldn't change without explicit deployment
- Customized installations where you've modified the templates
- Environments where file writes should be minimized

### Development vs Production Mode:

Development mode: Relaxed security and verbose debug information for testing

Production mode: Enforces HTTPS-only features including strict CSRF protection

### Administration

1. Access `/admin` and log in
2. Add RSS feeds
3. Configure settings:
   - Site title and appearance
   - Maximum posts to retain
   - Update interval
   - Header/footer customization
   - Analytics/tracking code integration
   - **Enhanced Entry Display:**
     - Show blog/feed names as prefixes to entry titles
     - Display entry body text previews below titles (50-1000 characters)
     - Both features can be configured independently
4. Manage feeds:
   - Add/remove feeds
   - Preview feed content before adding
5. Backup/restore:
   - Export settings and feed lists
   - Import configuration from backup

## License

MIT License

Copyright (c) 2024 disinfo.zone

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.


## Acknowledgments

- Built with assistance from Anthropic's Claude
- Updates with Google's Jules
- [gofeed](https://github.com/mmcdole/gofeed) for RSS parsing
- [go-sqlite3](https://github.com/mattn/go-sqlite3) for database operations
- [Illuminati icon]((https://www.flaticon.com/free-icons/illuminati)) created by smalllikeart - Flaticon
