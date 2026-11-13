# Front-end build image. Used by `make frontend` / `make test-frontend`; the
# host needs only Docker.
#   docker build --target export --output type=local,dest=dist .
#   docker build --build-arg BUILD_SCRIPT=build:obfuscated --target export ...
#   docker build --target test .
FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
RUN if [ -f package-lock.json ]; then npm ci --no-audit --no-fund; \
    else npm install --no-audit --no-fund; fi
COPY . .

FROM deps AS test
RUN npm test

FROM deps AS build
ARG BUILD_SCRIPT=build
RUN npm run ${BUILD_SCRIPT}

# Only the generated files leave the builder.
FROM scratch AS export
COPY --from=build /app/dist/ /
