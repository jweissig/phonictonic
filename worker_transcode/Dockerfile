FROM gcr.io/phonic-tonic/ffmpeg:f8c139e
RUN apk add --no-cache ca-certificates
RUN mkdir -p /app && mkdir -p /app/staging && mkdir -p /app/tmp
ADD gopath/bin/web /app/web
WORKDIR /app
ENTRYPOINT ["/app/web"]
