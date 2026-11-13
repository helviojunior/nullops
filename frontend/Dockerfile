# Front-end build image. Used by `make frontend`; the host needs only Docker.
#   docker build --target export --output type=local,dest=dist .
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN if [ -f package-lock.json ]; then npm ci --no-audit --no-fund; \
    else npm install --no-audit --no-fund; fi
COPY . .
RUN npm run build

# Only the generated files leave the builder.
FROM scratch AS export
COPY --from=build /app/dist/ /
