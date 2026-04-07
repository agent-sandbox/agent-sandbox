# Production stage
FROM gcr.io/distroless/static:nonroot

COPY ./config/ /config/
COPY ./agent-sandbox /app
COPY ./ui/dist /ui/dist

CMD ["/app"]
