# syntax=docker/dockerfile:1
#
# tf-automation frontend: the built React app served by nginx, which also
# proxies /api/* to the backend so the browser sees a single origin (the
# session cookie needs that).
#
#   docker build -t tf-automation-frontend ./frontend
#   docker run -p 8080:80 -e BACKEND_URL=http://backend:8080 tf-automation-frontend

FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
# The official image renders /etc/nginx/templates/*.template with envsubst at
# startup, substituting only variables that are set (so nginx's own $host etc.
# are left alone).
ENV BACKEND_URL=http://backend:8080
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
