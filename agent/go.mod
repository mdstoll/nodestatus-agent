module nodestatus

go 1.24

// Vaste toolchain: de agent is een TLS/HTTP-server, dus beveiligingsfixes in
// crypto/tls en net/http komen alleen mee als we er bewust op bouwen. Zonder
// deze regel hing elke release af van welke Go er toevallig op deze Mac stond
// (0.2.18 werd gebouwd met 1.26.7 en miste zo de fixes van 1.26.9). Met
// GOTOOLCHAIN=auto (de standaard) haalt `go` deze versie zelf op.
toolchain go1.26.9
