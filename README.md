# Pushover Notify

A self-hosted scheduled notification reminder service that sends push notifications to your phone via [Pushover](https://pushover.net/). Perfect for GTD (Getting Things Done) workflows to help you stay on top of your tasks.

## Features

- **Scheduled Push Notifications** - Set specific times to receive reminders
- **Repeated Reminders** - Customizable repeat times and intervals to ensure you never miss important tasks
- **Modern Web UI** - Clean interface built with HTMX + Tailwind CSS with real-time updates
- **Real-time Sync** - Server-Sent Events (SSE) for instant data synchronization across browser tabs
- **Lightweight Deployment** - Single binary, JSON file storage, no database required
- **Container Ready** - Includes Containerfile for Podman/Docker deployment

## Screenshots

```
┌─────────────────────────────────────────────────────────────┐
│  Pushover Notify                     Settings | Logout      │
├─────────────────────────────────────────────────────────────┤
│  Add Notification                                           │
│  ┌─────────────────┐  ┌─────────────────────────────────┐  │
│  │ Scheduled Time  │  │ Content                         │  │
│  │ 2024-01-30 09:00│  │ Review project proposal         │  │
│  └─────────────────┘  └─────────────────────────────────┘  │
│  ┌─────────────────┐  ┌─────────────────────────────────┐  │
│  │ Repeat Times: 3 │  │ Repeat Interval: 30 Minutes     │  │
│  └─────────────────┘  └─────────────────────────────────┘  │
│                                      [Add Notification]     │
├─────────────────────────────────────────────────────────────┤
│  Scheduled Notifications                                    │
│  ┌───────────┬──────────────────┬─────────┬────┬──────────┐│
│  │ Time      │ Content          │ Status  │Sent│ Actions  ││
│  ├───────────┼──────────────────┼─────────┼────┼──────────┤│
│  │ 09:00 AM  │ Morning standup  │ Pending │ 0  │ Edit Del ││
│  │ 02:00 PM  │ Code review      │ Done    │ 3  │      Del ││
│  └───────────┴──────────────────┴─────────┴────┴──────────┘│
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

1. Sign up for a [Pushover](https://pushover.net/) account
2. Create an Application in Pushover to get your **API Token**
3. Get your **User Key** from your Pushover dashboard

### Run Locally

```bash
# Clone the repository
git clone https://github.com/noahxzhu/pushover-notify.git
cd pushover-notify

# Run the server
go run cmd/server/main.go
```

Visit http://localhost:8089. You'll be prompted to set a password on first access.

### Container Deployment (Recommended)

Deploy with Podman Quadlet (systemd managed):

```bash
# Build and deploy
./deploy/deploy.sh
```

Or manually with Docker/Podman:

```bash
# Build the image
podman build -t pushover-notify:latest -f Containerfile .

# Run the container
podman run -d \
  --name pushover-notify \
  -p 8089:8089 \
  -v pushover-notify-data:/app/data \
  -e TZ=Asia/Shanghai \
  pushover-notify:latest
```

## Configuration

### Config File

`configs/config.yaml`:

```yaml
server:
  port: ":8089"

storage:
  file_path: "data/data.json"
```

### Web Interface Setup

On first access:

1. **Set Password** - Secure your web interface
2. **Configure Pushover** - Go to Settings and enter:
   - User Key
   - App Token
3. **Set Defaults** - Configure default repeat times and interval

## Usage

### Adding a Notification

1. Select **Scheduled Time** - When to send the first reminder
2. Enter **Content** - Your reminder message
3. Set **Repeat Times** - How many times to send the reminder (default: 3)
4. Set **Repeat Interval** - Time between reminders (e.g., 30 minutes)
5. Click **Add Notification**

### Notification Status

| Status | Description |
|--------|-------------|
| Pending | Waiting to be sent / Still sending reminders |
| Done | Completed (sent all reminders) |

### How It Works

1. When the scheduled time arrives, the first reminder is sent
2. The reminder repeats at the configured interval
3. After all repeat times are exhausted, the status changes to Done

## Project Structure

```
pushover-notify/
├── cmd/server/          # Application entry point
├── configs/             # Configuration files
├── deploy/              # Deployment scripts
├── internal/
│   ├── config/          # Config loading
│   ├── model/           # Data models
│   ├── pushover/        # Pushover API client
│   ├── storage/         # JSON file storage
│   ├── web/             # Web server & templates
│   └── worker/          # Background task processing
├── Containerfile        # Container build file
└── README.md
```

## Tech Stack

- **Backend**: Go 1.24+
- **Frontend**: HTMX + Tailwind CSS (CDN)
- **Real-time Updates**: Server-Sent Events (SSE)
- **Storage**: JSON file
- **Deployment**: Podman Quadlet / Docker

## License

MIT
