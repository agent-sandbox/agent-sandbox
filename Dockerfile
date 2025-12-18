# Production stage
FROM centos:7

COPY ./agent-sandbox /app
RUN chmod +x /app

CMD /app
