FROM golang:1.22-bookworm

WORKDIR /agent/src

# Copy source and build
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /agent/brain .

# Copy default agent data
COPY agent/ /agent/defaults/

# Copy entrypoint
COPY entrypoint.sh /agent/entrypoint.sh
RUN chmod +x /agent/entrypoint.sh

# Also copy Dockerfile into the image so the agent can read/edit it
COPY Dockerfile /agent/src/Dockerfile

EXPOSE 8377

ENTRYPOINT ["/agent/entrypoint.sh"]
