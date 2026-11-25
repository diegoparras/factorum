# Factorum: binario Go estático + poppler (PDF), tesseract (OCR) y zbar (QR de respaldo).
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# La versión sale del CHANGELOG (primera sección "## [X.Y.Z]"): sin novedades no hay versión.
RUN V="$(sed -n 's/^## \[\([0-9][0-9.]*\)\].*/\1/p' CHANGELOG.md | head -1)" && test -n "$V" && \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/diegoparras/contadorfacturas/internal/config.Version=$V" \
      -o /out/factorum ./cmd/factorum

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      poppler-utils tesseract-ocr tesseract-ocr-spa zbar-tools ca-certificates tzdata && \
    rm -rf /var/lib/apt/lists/* && \
    useradd -r -u 10001 -m -d /home/factorum factorum && mkdir -p /data && chown factorum /data
COPY --from=build /out/factorum /usr/local/bin/factorum
USER factorum
ENV TZ=America/Argentina/Buenos_Aires FACTORUM_DATOS=/data FACTORUM_PUERTO=8095
VOLUME /data
EXPOSE 8095
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["factorum", "salud"]
ENTRYPOINT ["factorum"]
CMD ["serve"]
