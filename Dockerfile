# ---- UI ----
FROM node:22-alpine AS ui
WORKDIR /ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ---- Go ----
FROM golang:1.24-alpine AS build
ARG REVISION=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
# The UI is embedded in the binary via go:embed.
RUN rm -rf internal/server/ui-dist
COPY --from=ui /ui/dist internal/server/ui-dist
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/slashsre/slashsre-devops-controller/internal/version.REVISION=${REVISION}" \
    -o /out/manager ./cmd

# ---- Runtime ----
FROM gcr.io/distroless/static:nonroot
ARG VERSION
ARG REVISION
LABEL org.opencontainers.image.title="slashsre-devops-controller" \
      org.opencontainers.image.description="Kubernetes controller for node lifecycle, queue-triggered jobs, alert remediation and post-deploy tests" \
      org.opencontainers.image.source="https://github.com/slashsre/slashsre-devops-controller" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
WORKDIR /
COPY --from=build /out/manager /manager
USER 65532:65532
EXPOSE 9898
ENTRYPOINT ["/manager"]
