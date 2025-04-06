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

Make a GET request to the root path:

```
GET http://localhost:8080/
```

The response will be JSON in this format:

```json
{
  "target_amount": 1000,
  "total_amount": "750.00"
}
```

## Testing with HTML frontend

1. Start the proxy server:
   ```
   go run proxy.go -port 8080
   ```

2. Open the `index.html` file in a browser. You can do this directly from the file system or serve it using a simple HTTP server:
   ```
   python3 -m http.server 8000
   ```
   Then navigate to `http://localhost:8000/index.html`

3. The page will display the total amount and automatically refresh every 30 seconds.

4. Ensure the proxy server is running on the same port specified in the JavaScript code (`http://localhost:8080`).

## Configuration

The proxy is configured to fetch data from a specific iRaiser endpoint with required headers.
