FROM node:22-trixie-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        sudo \
        tini \
    && rm -rf /var/lib/apt/lists/*

# change name node to user
RUN usermod -l user -d /home/user -m node && \
    groupmod -n user node && \
    chown -R user:user /home/user && \
    chmod -R 700 /home/user && \
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
