# Lework Development Environment

Docker Compose setup for Lework development environment.

## Services

This setup brings up the following services:
- **lework**: Main application server
- **postgresql**: Database server
- **nats**: Message broker with JetStream

## Usage

Start all services:
```bash
docker-compose up
```

Start all services in background:
```bash
docker-compose up -d
```

Stop all services:
```bash
docker-compose down
```

View logs:
```bash
docker-compose logs -f
```

## Ports

- `8080`: Lework API server
- `5432`: PostgreSQL server
- `4222`: NATS server (Client)
- `8222`: NATS Monitoring

## Health Checks

The services have built-in health checks to ensure they are ready before dependent services start.