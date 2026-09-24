import { Controller, Get, Injectable, Module } from '@nestjs/common';
import { HealthCheck, HealthCheckService, HealthIndicatorService, TerminusModule } from '@nestjs/terminus';
import type { Knex } from 'knex';
import { Public } from '../auth/decorators';
import { InjectKnex } from '../database/database.module';

//guide:health
// KnexHealthIndicator checks that the database answers.
@Injectable()
export class KnexHealthIndicator {
  constructor(
    @InjectKnex() private readonly knex: Knex,
    private readonly health: HealthIndicatorService,
  ) {}

  async ping(key: string) {
    const indicator = this.health.check(key);
    try {
      await this.knex.raw('select 1');
      return indicator.up();
    } catch {
      return indicator.down();
    }
  }
}

@Public()
@Controller('health')
export class HealthController {
  constructor(
    private readonly health: HealthCheckService,
    private readonly db: KnexHealthIndicator,
  ) {}

  @Get('live')
  live() {
    return { status: 'ok' };
  }

  @Get('ready')
  @HealthCheck()
  ready() {
    return this.health.check([() => this.db.ping('db')]);
  }
}
//guide:end

@Module({
  imports: [TerminusModule],
  controllers: [HealthController],
  providers: [KnexHealthIndicator],
})
export class HealthModule {}
