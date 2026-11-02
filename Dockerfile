# Local run of the landing site: build with the same script Netlify uses, serve with nginx.

FROM node:22-alpine AS build
WORKDIR /app
COPY scripts ./scripts
COPY landing ./landing
ARG FOUNDER_NAME
ARG CONTACT_EMAIL
ARG CONTACT_PHONE
ARG LEGAL_ENTITY
ARG LEGAL_ENTITY_FR
ARG LEGAL_SEAT
ARG LEGAL_SEAT_FR
ARG LEGAL_DETAILS
ARG LEGAL_DETAILS_FR
ARG SITE_URL=http://localhost:8080
# "local" lets the build fall back to placeholders for any value left empty.
ENV CONTEXT=local \
    FOUNDER_NAME=$FOUNDER_NAME \
    CONTACT_EMAIL=$CONTACT_EMAIL \
    CONTACT_PHONE=$CONTACT_PHONE \
    LEGAL_ENTITY=$LEGAL_ENTITY \
    LEGAL_ENTITY_FR=$LEGAL_ENTITY_FR \
    LEGAL_SEAT=$LEGAL_SEAT \
    LEGAL_SEAT_FR=$LEGAL_SEAT_FR \
    LEGAL_DETAILS=$LEGAL_DETAILS \
    LEGAL_DETAILS_FR=$LEGAL_DETAILS_FR \
    SITE_URL=$SITE_URL
RUN node scripts/build.mjs

FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY docker/security-headers.conf /etc/nginx/snippets/security-headers.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD wget -qO /dev/null http://127.0.0.1:8080/ || exit 1
