export const DOCKER_COMPOSE_TEMPLATE = `name: zerly-infrastructure

services:
  # ---------------------------------------------------------------------------
  # PostgreSQL (Database)
  # ---------------------------------------------------------------------------
  postgres:
    image: postgres:18-alpine
    container_name: zerly-postgres
    restart: always
    ports:
      - "5432:5432"
    environment:
      POSTGRES_USER: zerly
      POSTGRES_PASSWORD: password
      POSTGRES_DB: zerly_db
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - zerly-network
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U zerly -d zerly_db"]
      interval: 5s
      timeout: 5s
      retries: 5

  # ---------------------------------------------------------------------------
  # NATS (Message Broker + JetStream)
  # ---------------------------------------------------------------------------
  nats:
    image: nats:alpine
    container_name: zerly-nats
    restart: always
    # -js: Enable JetStream
    # -sd: Store Data directory (for persistence)
    # --max_payload: Increase max payload to 8MB
    command: -js -sd /data -n zerly-nats
    ports:
      - "4222:4222" # Client connection
      - "8222:8222" # Dashboard / Monitoring
    volumes:
      - nats_data:/data
    networks:
      - zerly-network
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8222/healthz"]
      interval: 5s
      timeout: 5s
      retries: 5

  # ---------------------------------------------------------------------------
  # Garnet (Cache / Redis compatible)
  # ---------------------------------------------------------------------------
  garnet:
    image: ghcr.io/microsoft/garnet:latest
    container_name: zerly-garnet
    restart: always
    ports:
      - "6379:6379" # Standard Redis port
    volumes:
      - garnet_data:/data
    networks:
      - zerly-network

networks:
  zerly-network:
    driver: bridge
    name: zerly-network

volumes:
  postgres_data:
  nats_data:
  garnet_data:
`;
