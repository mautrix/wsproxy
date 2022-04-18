# mautrix-wsproxy
A simple HTTP push -> websocket proxy for Matrix appservices.

This is used by [mautrix-imessage](https://github.com/mautrix/imessage)
to receive appservice transactions without opening a port to the local Mac
where the bridge runs. In general, you should run wsproxy in the same place as
your homeserver and use your existing reverse proxy to secure the websocket
connection between the bridge and wsproxy.

![flowchart](./flowchart.png)

## Setup
You can download a prebuilt executable from [the CI] or [GitHub releases]. The
executables are statically compiled and have no dependencies. Alternatively,
you can build from source:

0. Have [Go](https://golang.org/) 1.16 or higher installed.
1. Clone the repository (`git clone https://github.com/mautrix/wsproxy.git`).
2. Build with `go build -o mautrix-wsproxy`. The resulting executable will be
   in the current directory named `mautrix-wsproxy`.

After you have the executable ready, configure and run mautrix-wsproxy:

1. Copy `example-config.yaml` from the root of the repo to `config.yaml`
   and fill out the fields (with the tokens from the registration file).
2. Change the appservice registration file to point your homeserver at
   mautrix-wsproxy. The registration's `url` field should have a `http://`
   address that your homeserver can use to reach mautrix-wsproxy. Restart
   homeserver after registration changes.
3. Change the bridge config (`homeserver` -> `websocket_proxy`)
   to point at mautrix-wsproxy (the URL should have `ws://` or `wss://`).
4. Run the proxy with `mautrix-wsproxy` and start the bridge.

[the CI]: https://mau.dev/mautrix/wsproxy/-/pipelines
[GitHub releases]: https://github.com/mautrix/wsproxy/releases

## Sample docker-compose file
The compose files here also include [mautrix-syncproxy]. It's mostly needed for
the Android SMS bridge (to save battery), but can be used with iMessage as well.
You can omit it if you don't use the `bridge`→`encryption`→`appservice` option.

[mautrix-syncproxy]: https://github.com/mautrix/syncproxy

```yaml
version: "3.7"

services:
  mautrix-wsproxy:
    container_name: mautrix-wsproxy
    image: dock.mau.dev/mautrix/wsproxy
    restart: unless-stopped
    ports:
      - 29331
    environment:
      #LISTEN_ADDRESS: ":29331"
      APPSERVICE_ID: imessage
      AS_TOKEN: put your as_token here
      HS_TOKEN: put your hs_token here
      # These URLs will work as-is with docker networking
      SYNC_PROXY_URL: http://mautrix-syncproxy:29332
      SYNC_PROXY_WSPROXY_URL: http://mautrix-wsproxy:29331
      SYNC_PROXY_SHARED_SECRET: random string here

  mautrix-syncproxy:
    container_name: mautrix-syncproxy
    image: dock.mau.dev/mautrix/syncproxy
    restart: unless-stopped
    environment:
      #LISTEN_ADDRESS: ":29332"
      DATABASE_URL: postgres://user:pass@host/mautrixsyncproxy
      HOMESERVER_URL: http://localhost:8008
      SHARED_SECRET: same random string as above here
```

### Docker with multiple appservices
The environment variables only support one appservice at a time, so you'll need
to use a config file if you want more:

```yaml
version: "3.7"

services:
  mautrix-wsproxy:
    container_name: mautrix-wsproxy
    image: dock.mau.dev/mautrix/wsproxy
    restart: unless-stopped
    command: /usr/bin/mautrix-wsproxy -config /data/config.yaml
    volumes:
    - ./config:/data
    ports:
    - 29331
    environment:
      #LISTEN_ADDRESS: ":29331"

  mautrix-syncproxy:
    container_name: mautrix-syncproxy
    image: dock.mau.dev/mautrix/syncproxy
    restart: unless-stopped
    environment:
      #LISTEN_ADDRESS: ":29332"
      DATABASE_URL: postgres://user:pass@host/mautrixsyncproxy
      HOMESERVER_URL: http://localhost:8008
      SHARED_SECRET: random string here
```
