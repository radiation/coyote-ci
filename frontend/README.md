# Coyote CI Frontend

React + TypeScript + Vite UI for listing builds, viewing build details, and queueing new builds with a template.

## Prerequisites

- Node.js 20+
- npm
- Backend API running (default: http://localhost:8080)

## Local development

1. Install dependencies:

```bash
npm install
```

2. Configure environment:

```bash
cp .env.example .env
```

3. Start the frontend dev server:

```bash
npm run dev
```

The app runs on http://localhost:3000.

## API configuration

- Vite dev proxy target: `VITE_API_BASE` (used by `vite.config.ts`)
  - Default: `http://localhost:8080`
- Browser API base path: `VITE_API_BASE_PATH` (used by `src/api/client.ts`)
  - Default: `/api`

Recommended local setup keeps `VITE_API_BASE_PATH=/api` and uses the Vite proxy.

## Commands

```bash
npm run dev
npm test -- --run
npm run build
npm run lint
```
