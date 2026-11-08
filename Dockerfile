# Build: docker build -t roost .
# Run:   docker run -v /srv/documents:/docs:ro -v roost-data:/data \
#          -v "$PWD/roost.toml:/etc/roost/roost.toml:ro" -p 8484:8484 roost
FROM golang:1.26 AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /roost ./cmd/roost

# The binary is static, so the image holds nothing else.
FROM scratch
COPY --from=build /roost /roost
USER 65532:65532
ENV ROOST_CONFIG=/etc/roost/roost.toml
EXPOSE 8484
ENTRYPOINT ["/roost"]
CMD ["serve"]
