# iRaiser Proxy

A simple proxy server that retrieves fundraising data from iRaiser API and returns only the target and total amounts.

## Features

- Caches responses for 5 seconds to reduce load on the upstream API
- Handles CORS for cross-origin requests
- Returns simplified JSON with only the necessary data

## Usage

### Run from source

Start the server with an optional port parameter:

```
go run proxy.go -port 8080
```

### Build and run for linux server

Build the binary:

```
env GOOS=linux GOARCH=amd64 go build proxy.go
```

Make it executable and run:

```
chmod +x proxy
./proxy -port 8080
```

## API

- `GET /` serves a UI: paste a fundraiser page and it gives you the API URL for it.
- `GET /kentaa` returns the preconfigured fundraiser.
- `GET /fundraiser?url=<fundraiser page>` returns any iRaiser (Kentaa) fundraiser, team or campaign, or a Securycast, Nenäpäivä or Mielipotti fundraiser (URL-encode the page), e.g.

  ```
  GET http://localhost:8080/fundraiser?url=https%3A%2F%2Foma.wwf.fi%2Ffundraisers%2Fvauhtijuoksuplus2025
  ```

Both JSON endpoints respond in this format:

```json
{
  "target_amount": 1000,
  "total_amount": "750.00"
}
```

## Configuration

The proxy is configured to fetch data from a specific iRaiser endpoint with required headers.
