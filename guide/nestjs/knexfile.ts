import type { Knex } from 'knex';

// The configuration of the Knex CLI, for the migrations: npm run migrate.
const config: Knex.Config = {
  client: 'pg',
  connection: process.env.DATABASE_URL,
  migrations: { directory: 'migrations' },
};

export default config;
