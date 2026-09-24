import { ConflictException, Injectable, NotFoundException } from '@nestjs/common';
import type { Knex } from 'knex';
import { DatabaseError } from 'pg';
import { v7 as uuidv7 } from 'uuid';
import { decodeCursor, encodeCursor, Page } from '../common/page';
import { InjectKnex } from '../database/database.module';
import { CreateProjectDto, ListProjectsQuery, ProjectDto } from './project.dto';

// ProjectRow is a row of the table projects.
export interface ProjectRow {
  id: string;
  owner_id: string;
  key: string;
  name: string;
  last_number: number;
  created_at: Date;
}

// ProjectsService keeps the projects of their owners: a caller sees its
// own projects only.
@Injectable()
export class ProjectsService {
  constructor(@InjectKnex() private readonly knex: Knex) {}

  //guide:create-project
  async create(owner: string, dto: CreateProjectDto): Promise<ProjectDto> {
    try {
      const [row] = await this.knex<ProjectRow>('projects')
        .insert({ id: uuidv7(), owner_id: owner, key: dto.key, name: dto.name })
        .returning('*');
      return toProject(row);
    } catch (error) {
      if (error instanceof DatabaseError && error.code === '23505') {
        throw new ConflictException(`a project with the key ${dto.key} exists`);
      }
      throw error;
    }
  }
  //guide:end

  //guide:get-project
  async get(owner: string, id: string): Promise<ProjectDto> {
    const row = await this.knex<ProjectRow>('projects').where({ id, owner_id: owner }).first();
    if (!row) {
      throw new NotFoundException(`project ${id} not found`);
    }
    return toProject(row);
  }
  //guide:end

  async list(owner: string, query: ListProjectsQuery): Promise<Page<ProjectDto>> {
    const after = decodeCursor(query.cursor);
    const limit = query.limit ?? 20;
    const rows = await this.knex<ProjectRow>('projects')
      .where({ owner_id: owner })
      .modify((q) => {
        if (after) {
          q.where('id', '<', after);
        }
      })
      .orderBy('id', 'desc')
      .limit(limit + 1);
    const page: Page<ProjectDto> = { items: rows.slice(0, limit).map(toProject) };
    if (rows.length > limit) {
      page.next_cursor = encodeCursor(rows[limit - 1].id);
    }
    return page;
  }

  async remove(owner: string, id: string): Promise<void> {
    let deleted: number;
    try {
      deleted = await this.knex('projects').where({ id, owner_id: owner }).delete();
    } catch (error) {
      if (error instanceof DatabaseError && error.code === '23503') {
        throw new ConflictException(`project ${id} has tasks: delete them first`);
      }
      throw error;
    }
    if (deleted === 0) {
      throw new NotFoundException(`project ${id} not found`);
    }
  }
}

function toProject(row: ProjectRow): ProjectDto {
  return { id: row.id, key: row.key, name: row.name, created_at: row.created_at };
}
