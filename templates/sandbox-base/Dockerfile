FROM python:3.12-slim-trixie

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        sudo \
        tini \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd -g 1000 user && \
    useradd -u 1000 -g 1000 -m -s /bin/bash user && \
    chown -R user:user /home/user && \
    chmod -R 777 /home/user && \
    echo 'user ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers && \
    echo 'Defaults:user !requiretty' >> /etc/sudoers

COPY envd-0.2.10 /usr/bin/envd
RUN chmod +x /usr/bin/envd

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8

EXPOSE 49983

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD []
