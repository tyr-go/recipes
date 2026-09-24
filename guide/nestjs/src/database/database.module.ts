import { Global, Inject, Module, OnApplicationShutdown } from '@nestjs/common';
import knex, { Knex } from 'knex';

export const KNEX = Symbol('KNEX');

// InjectKnex injects the Knex of the database.
export const InjectKnex = () => Inject(KNEX);

@Global()
@Module({
  providers: [
    {
      provide: KNEX,
      useFactory: (): Knex => knex({ client: 'pg', connection: process.env.DATABASE_URL, pool: { min: 0, max: 10 } }),
    },
  ],
  exports: [KNEX],
})
export class DatabaseModule implements OnApplicationShutdown {
  constructor(@InjectKnex() private readonly knex: Knex) {}

  async onApplicationShutdown(): Promise<void> {
    await this.knex.destroy();
  }
}
