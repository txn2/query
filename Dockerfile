FROM scratch
ENV PATH=/bin

COPY query /bin/

WORKDIR /

ENTRYPOINT ["/bin/query"]