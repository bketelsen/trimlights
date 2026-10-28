# Build each target with GOOS=linux GOARCH=amd64 (or arm64) make build.
# The binary embeds timezone data and Go's public fallback CA bundle.
FROM scratch
ARG TARGETARCH=amd64
COPY build/${TARGETARCH}/trimlight /trimlight
USER 65532:65532
ENTRYPOINT ["/trimlight"]
CMD ["serve", "--help"]
