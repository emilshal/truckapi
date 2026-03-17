# TruckAPI: C.H. Robinson Integration

TruckAPI is a Go-based application that integrates with the C.H. Robinson API to allow truck drivers to bid on orders, track orders within 250 miles of their current zip code, communicate their location every 30 minutes, and track route metrics.

## Getting Started

These instructions will get you a copy of the project up and running on your local machine for development and testing purposes.

### Prerequisites

What things you need to install the software and how to install them:

- Go (version 1.15 or later)

### Installing

A step by step series of examples that tell you how to get a development environment running:

1. Clone the repository:

```
bash
git clone https://github.com/yourusername/TruckAPI.git
cd TruckAPI
```

2. Set up the environment variables

```
CHROB_CLIENT_ID - Our client ID we POST to CHRobinson /v1/oauth/token
CHROB_CLIENT_SECRET - Our secret we POST to CHRobinson /v1/oauth/token
CHROB_TOKEN_URL - The URL for the Oauth Token
CHROB_AUDIENCE - "https://inavisphere.chrobinson.com"
CHROB_GRANT_TYPE=client_credentials
CHROB_BASE_URL - The base of the CHRobinson URL we are interacting with https://api.chrobinson.com
SERVER_LISTEN_ADDR=:8080
CHRobAccessToken - The token url stored in the environment variable

```

3. Build & run the application from truckapi/cmd/truckapi/main.go

```
go build -o truckapi cmd/truckapi/main.go
./truckapi
```

## CHRob LoaderAPI Dedupe Deployment Notes

To prevent duplicate CHRob orders being re-posted to LoaderAPI, deploy with these rules:

- Run only one `truckapi` instance/container at a time (no duplicate processes).
- Do not override CHRob send dedupe TTL; it is hardcoded in code to 24h (`1440` minutes).

Why this matters:

- CHRob resend suppression is in-memory only for a 24-hour window while the process is running.
- A restart clears that in-memory cache, so old loads can be sent again unless LoaderAPI also dedupes.

Practical Docker/Compose guidance:

- Keep the service at a single replica/container only.
- Avoid creating local snapshot copies of runtime data in `.tmp`; the helper scripts now use temporary directories and clean them up automatically.

Bonus (recommended):

- Have LoaderAPI dedupe incoming CHRob posts by `source + orderNumber` as defense in depth.
