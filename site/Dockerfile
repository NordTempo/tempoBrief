# brief.nordtempo.com. The build context is site/ (Dock deploys it on every
# push to main with `dock git connect NordTempo/tempoBrief --dir site`).
# Same Caddy base as tempoDock's site, pinned by digest.
FROM caddy:2-alpine@sha256:003a97808e781aa9f7e75cc2f43d34d461099031faf5e20faefb9f61126836f0
COPY Caddyfile /etc/caddy/Caddyfile
COPY index.html /srv/
COPY fonts /srv/fonts
EXPOSE 8080
