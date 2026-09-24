import { BadRequestException, Injectable, NotFoundException } from '@nestjs/common';
import type { Knex } from 'knex';
import { v7 as uuidv7 } from 'uuid';
import { decodeCursor, encodeCursor, Page } from '../common/page';
import { InjectKnex } from '../database/database.module';
import type { ProjectRow } from '../projects/projects.service';
import { ProjectsService } from '../projects/projects.service';
import { CreateTaskDto, ListTasksQuery, Status, TaskDto, UpdateTaskDto } from './task.dto';

// TaskRow is a row of the table tasks.
interface TaskRow {
  id: string;
  project_id: string;
  number: number;
  title: string;
  status: Status;
  due_at: Date | null;
  created_at: Date;
  updated_at: Date;
}

// TasksService keeps the tasks of the projects of their owners.
@Injectable()
export class TasksService {
  constructor(
    @InjectKnex() private readonly knex: Knex,
    private readonly projects: ProjectsService,
  ) {}

  //guide:create-task
  async create(owner: string, projectId: string, dto: CreateTaskDto): Promise<TaskDto> {
    return this.knex.transaction(async (trx) => {
      // Numbering the task locks the project until the transaction ends,
      // and finds no project unless it's the caller's.
      const [project] = await trx<ProjectRow>('projects')
        .where({ id: projectId, owner_id: owner })
        .increment('last_number', 1)
        .returning('last_number');
      if (!project) {
        throw new NotFoundException(`project ${projectId} not found`);
      }
      const [row] = await trx<TaskRow>('tasks')
        .insert({
          id: uuidv7(),
          project_id: projectId,
          number: project.last_number,
          title: dto.title,
          status: dto.status ?? 'todo',
          due_at: dto.due_at ?? null,
        })
        .returning('*');
      return toTask(row);
    });
  }
  //guide:end

  async get(owner: string, id: string): Promise<TaskDto> {
    const row = await this.ofOwner(owner).where('tasks.id', id).first();
    if (!row) {
      throw new NotFoundException(`task ${id} not found`);
    }
    return toTask(row);
  }

  //guide:list-tasks
  async list(owner: string, projectId: string, query: ListTasksQuery): Promise<Page<TaskDto>> {
    // The project of another owner is not found, rather than empty.
    await this.projects.get(owner, projectId);
    const after = decodeCursor(query.cursor);
    const limit = query.limit ?? 20;
    const rows = await this.knex<TaskRow>('tasks')
      .where({ project_id: projectId })
      .modify((q) => {
        if (query.status) {
          q.where({ status: query.status });
        }
        if (after) {
          q.where('id', '<', after);
        }
      })
      .orderBy('id', 'desc')
      .limit(limit + 1); // one more than the page, to know if there's a next one
    const page: Page<TaskDto> = { items: rows.slice(0, limit).map(toTask) };
    if (rows.length > limit) {
      page.next_cursor = encodeCursor(rows[limit - 1].id);
    }
    return page;
  }
  //guide:end

  async update(owner: string, id: string, dto: UpdateTaskDto): Promise<TaskDto> {
    if (dto.title === undefined && dto.status === undefined && dto.due_at === undefined) {
      throw new BadRequestException('nothing to change: set title, status or due_at');
    }
    const [row] = await this.knex<TaskRow>('tasks')
      .where({ id })
      .whereIn('project_id', this.knex('projects').select('id').where({ owner_id: owner }))
      .update({ ...dto, updated_at: this.knex.fn.now() })
      .returning('*');
    if (!row) {
      throw new NotFoundException(`task ${id} not found`);
    }
    return toTask(row);
  }

  async remove(owner: string, id: string): Promise<void> {
    const deleted = await this.knex('tasks')
      .where({ id })
      .whereIn('project_id', this.knex('projects').select('id').where({ owner_id: owner }))
      .delete();
    if (deleted === 0) {
      throw new NotFoundException(`task ${id} not found`);
    }
  }

  // ofOwner selects the tasks of the projects of owner.
  private ofOwner(owner: string) {
    return this.knex<TaskRow>('tasks')
      .select('tasks.*')
      .join('projects', 'projects.id', 'tasks.project_id')
      .where('projects.owner_id', owner);
  }
}

function toTask(row: TaskRow): TaskDto {
  return {
    id: row.id,
    project_id: row.project_id,
    number: row.number,
    title: row.title,
    status: row.status,
    due_at: row.due_at ?? undefined,
    created_at: row.created_at,
    updated_at: row.updated_at,
  };
}
