# Infoscope

Infoscope is a minimalist public RSS river reader designed to reduce information anxiety while helping users discover interesting content. It reimagines how we consume online information by treating content as a flowing river rather than an accumulating inbox.

## The Problem

Traditional RSS readers can create anxiety:
- Unread counts become a constant reminder of "falling behind"
- The pressure to "catch up" turns reading into a chore
- Private feed lists keep valuable curation hidden from others
- Infinite archives make it hard to "let go" of old content

## The Solution

Infoscope approaches these challenges differently:

### The River Metaphor
Instead of treating content as something to be collected and archived, Infoscope presents it as a flowing river. Like a real river, you can:
- Stop by whenever you want to see what's flowing past
- Take what interests you in the moment
- Let the rest flow by without worry
- Return tomorrow to find fresh content

### Public Curation
Most RSS readers are private, treating feed selection as a personal choice. Infoscope takes a different approach. Feed selection becomes a form of public curation and curators can share their expertise through feed choices.

### Privacy-First Design
While curation is public, reader privacy is paramount. Infoscope has no required user accounts, no personal data collection, and no cookies for basic functionality. The only statistics collected (clicks on links) is fully anonymous with no user data recorded at all.

### Minimalist Interface
The interface is intentionally simple in keeping with the guiding ethos. It is a clean, distraction-free retro design with a focus on content discovery. This means no infinite scroll (admins define the number of links shown), no pagination, no unread counts, no retention of old entries.

## Running Infoscope

### Quick Start

A docker container as well as autobuilds of the go application are forthcoming, in the meantime you can build and deploy as follow:

```bash
# Download and build
git clone https://github.com/yourusername/infoscope.git
cd infoscope
go build ./cmd/infoscope

# Run
./infoscope
```

Visit `http://localhost:8080/setup` to complete installation. It should default to this page until an admin account has been created.

### Configuration

Command line flags:
- `-port`: HTTP port (default: 8080)
- `-db`: Database path (default: data/infoscope.db)
- `-version`: Print version information

Environment variables:
- `INFOSCOPE_PORT`: HTTP port
- `INFOSCOPE_DB_PATH`: Database path

### Administration

1. Access `/admin` and log in
2. Add RSS feeds
3. Configure settings:
   - Site title
   - Maximum posts to retain
   - Update interval
   - Visual customization

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
- [gofeed](https://github.com/mmcdole/gofeed) for RSS parsing
- [go-sqlite3](https://github.com/mattn/go-sqlite3) for database operations