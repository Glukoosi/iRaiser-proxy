# iRaiser Proxy

A small Go server that fetches a fundraiser's goal and total and returns them as simple JSON. It also serves a stream overlay widget for OBS.

Supports iRaiser (Kentaa) fundraisers, teams and campaigns (e.g. oma.wwf.fi, lahjoita.punainenristi.fi, oma.kummit.fi), plus Securycast, Nenäpäivä and Mielipotti. Responses are cached for 5 seconds and CORS is allowed.

## Run

```
go run proxy.go -port 8080
```

Build for a Linux server:

```
env GOOS=linux GOARCH=amd64 go build proxy.go
./proxy -port 8080
```

Test:

```
go test
```

## Endpoints

- `GET /` is a page where you paste a fundraiser address and get the API and widget URLs.
- `GET /fundraiser?url=<fundraiser page>` returns the amounts for any supported fundraiser (URL-encode the page address).
- `GET /widget?url=<fundraiser page>` is a transparent overlay showing the live total. Optional: `color=ffffff`, `size=64`, `font=Syncopate|Montserrat|Bebas+Neue|Press+Start+2P|system`, `goal=1`.
- `GET /kentaa` returns the built-in default fundraiser.

The JSON looks like this:

```json
{
  "target_amount": 1000,
  "total_amount": "750.00"
}
```
