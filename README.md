# mautrix-wsproxy
A simple HTTP push -> websocket proxy for Matrix appservices.

This is used by [mautrix-imessage](https://github.com/tulir/mautrix-imessage)
to receive appservice transactions without opening a port to the local Mac
where the bridge runs.

## Setup
You can download a prebuilt executable from [the CI] or [GitHub releases]. The
executables are statically compiled and have no dependencies. Alternatively,
you can build from source:

0. Have [Go](https://golang.org/) 1.13 or higher installed.
1. Clone the repository (`git clone https://github.com/tulir/mautrix-wsproxy.git`).
2. Build with `go build`. The resulting executable will be in the current
   directory named `mautrix-wsproxy`.

After you have the executable ready, configure and run mautrix-wsproxy:

1. Copy `example-config.yaml` from the root of the repo to `config.yaml`
   and fill out the fields.
2. Change the appservice registration file to point your homeserver at
   mautrix-wsproxy. Restart homeserver after registration changes.
3. Change the bridge config (`homeserver` -> `websocket_proxy`)
   to point at mautrix-wsproxy.
4. Run the proxy with `mautrix-wsproxy` and start the bridge.

[the CI]: https://mau.dev/tulir/mautrix-wsproxy/-/pipelines
[GitHub releases]: https://github.com/tulir/mautrix-wsproxy/releases

## Sample docker-compose file
```yaml
version: "3.7"

services:
  mautrix-wsproxy:
    container_name: mautrix-wsproxy
    image: dock.mau.dev/tulir/mautrix-wsproxy
    restart: unless-stopped
    ports:
      - 29331
    environment:
      #LISTEN_ADDRESS: ":29331"
      APPSERVICE_ID: imessage
      AS_TOKEN: put your as_token here
      HS_TOKEN: put your hs_token here
```

### Docker with multiple appservices
The environment variables only support one appservice at a time, so you'll need
to use a config file if you want more:

```yaml
version: "3.7"

services:
  mautrix-wsproxy:
    container_name: mautrix-wsproxy
    image: dock.mau.dev/tulir/mautrix-wsproxy
    restart: unless-stopped
    command: /usr/bin/mautrix-wsproxy -config /data/config.yaml
    volumes:
    - ./config:/data
    ports:
    - 29331
    environment:
      #LISTEN_ADDRESS: ":29331"
      APPSERVICE_ID: imessage
      AS_TOKEN: put your as_token here
      HS_TOKEN: put your hs_token here
```
