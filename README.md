# Bedrock UDP Proxy (Gophertunnel)

This project accepts Minecraft Bedrock connections over RakNet on the configured UDP port and creates a separate Bedrock/RakNet connection to the configured destination. It then forwards decoded Gophertunnel packets in both directions.

## Important authentication limitation

This is a protocol-level proxy, not a raw UDP NAT. Gophertunnel terminates the client's Bedrock/RakNet connection and creates a new upstream Bedrock connection. The upstream server therefore has to accept the login/authentication model produced by the proxy. This does not bypass Microsoft/Xbox authentication, Geyser authentication, bans, allow-lists, or other destination controls.

The code forwards the client's client/identity data where the Gophertunnel API permits it, but it cannot make a destination believe the original TCP/UDP socket is the same connection. For an online-mode Geyser server, verify that the server/Geyser configuration supports the authentication arrangement before using this proxy. A proxy cannot make an incompatible authentication setup work merely by forwarding packets.

## Version

The source pins `github.com/sandertv/gophertunnel v1.62.0`. Its current package documentation lists v1.62.0 and the upstream project documents that recent releases require Go 1.24 or newer.

## Files

- `main.go` - proxy implementation
- `go.mod` - Go module and pinned dependency versions
- `config.yml` - listener/destination configuration

## Build Linux amd64

Use Go 1.24 or newer:

```bash
go mod tidy
go build -trimpath -ldflags='-s -w' -o bedrock-proxy .
```

For a cross-build from another OS:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bedrock-proxy .
```

## FeatherPanel

Upload `bedrock-proxy` and `config.yml` to the server's root Files directory. The startup command is:

```bash
./bedrock-proxy
```

Make sure the FeatherPanel allocation exposes UDP 19132 and that the host firewall/provider firewall allows UDP 19132. The program itself listens on `0.0.0.0:19132`; players connect to the VPS public IP, for example `VPS_PUBLIC_IP:19132`.

## Destination

Set `destination.host` and `destination.port` to the Bedrock endpoint that the destination actually exposes. For a Java server, that normally means the UDP port where Geyser is listening, not the Java TCP port.

## Testing

1. Start the proxy.
2. Confirm the console says it is listening on `0.0.0.0:19132`.
3. In Minecraft Bedrock, add an external server using the VPS public IP and UDP port `19132`.
4. Connect.
5. Watch the proxy log and the destination server log together.

## Common errors

### Address already in use
Another process owns UDP 19132. Stop it or choose another panel allocation/port.

### Connection timed out
Check the FeatherPanel allocation, Docker/container port mapping, host firewall, provider firewall, and that the VPS is actually reachable on UDP 19132.

### Destination connection failed
Check DNS/IP, destination UDP port, routing, and whether the destination is actually running a Bedrock listener such as Geyser.

### Login/spawn failed
This usually indicates an authentication/protocol/configuration mismatch or a destination-side restriction. The proxy does not bypass those restrictions. Check the destination's Geyser/auth settings and logs.

### Java port used as destination
A Java server's normal TCP port is not a Bedrock/RakNet endpoint. Point the proxy at the Geyser UDP listener instead.
