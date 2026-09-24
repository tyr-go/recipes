import { Injectable } from '@nestjs/common';
import type { Knex } from 'knex';
import { InjectKnex } from '../database/database.module';
import { StatsDto } from './stats.dto';

// AdminService counts the projects and tasks of all owners.
@Injectable()
export class AdminService {
  constructor(@InjectKnex() private readonly knex: Knex) {}

  async stats(): Promise<StatsDto> {
    const [projects] = await this.knex('projects').count<{ count: string }[]>('* as count');
    const [tasks] = await this.knex('tasks').count<{ count: string }[]>('* as count');
    const [done] = await this.knex('tasks').where({ status: 'done' }).count<{ count: string }[]>('* as count');
    return { projects: Number(projects.count), tasks: Number(tasks.count), done: Number(done.count) };
  }
}
