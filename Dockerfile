FROM scratch
ARG TARGETARCH
COPY resleased-linux-${TARGETARCH} /resleased
EXPOSE 8080
ENTRYPOINT ["/resleased"]