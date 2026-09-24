import { Body, Controller, Delete, Get, HttpCode, Param, ParseUUIDPipe, Patch, Post, Query, Res } from '@nestjs/common';
import {
  ApiBearerAuth,
  ApiCreatedResponse,
  ApiNoContentResponse,
  ApiNotFoundResponse,
  ApiOkResponse,
  ApiOperation,
  ApiTags,
} from '@nestjs/swagger';
import type { Response } from 'express';
import { Caller, CurrentCaller } from '../auth/caller';
import type { Page } from '../common/page';
import { CreateTaskDto, ListTasksQuery, TaskDto, TaskPageDto, UpdateTaskDto } from './task.dto';
import { TasksService } from './tasks.service';

@ApiTags('tasks')
@ApiBearerAuth()
@Controller()
export class TasksController {
  constructor(private readonly tasks: TasksService) {}

  @Post('projects/:projectId/tasks')
  @ApiOperation({ summary: 'Create a task in a project', description: 'The task gets the next number of its project.' })
  @ApiCreatedResponse({ type: TaskDto })
  @ApiNotFoundResponse()
  async create(
    @CurrentCaller() caller: Caller,
    @Param('projectId', ParseUUIDPipe) projectId: string,
    @Body() dto: CreateTaskDto,
    @Res({ passthrough: true }) res: Response,
  ): Promise<TaskDto> {
    const task = await this.tasks.create(caller.id, projectId, dto);
    res.location(`/tasks/${task.id}`);
    return task;
  }

  @Get('projects/:projectId/tasks')
  @ApiOperation({ summary: 'List the tasks of a project', description: 'Newest first, a page at a time, of a status or of any.' })
  @ApiOkResponse({ type: TaskPageDto })
  @ApiNotFoundResponse()
  list(
    @CurrentCaller() caller: Caller,
    @Param('projectId', ParseUUIDPipe) projectId: string,
    @Query() query: ListTasksQuery,
  ): Promise<Page<TaskDto>> {
    return this.tasks.list(caller.id, projectId, query);
  }

  @Get('tasks/:id')
  @ApiOperation({ summary: 'Get a task' })
  @ApiOkResponse({ type: TaskDto })
  @ApiNotFoundResponse()
  get(@CurrentCaller() caller: Caller, @Param('id', ParseUUIDPipe) id: string): Promise<TaskDto> {
    return this.tasks.get(caller.id, id);
  }

  @Patch('tasks/:id')
  @ApiOperation({ summary: 'Change a task' })
  @ApiOkResponse({ type: TaskDto })
  @ApiNotFoundResponse()
  update(@CurrentCaller() caller: Caller, @Param('id', ParseUUIDPipe) id: string, @Body() dto: UpdateTaskDto): Promise<TaskDto> {
    return this.tasks.update(caller.id, id, dto);
  }

  @Delete('tasks/:id')
  @HttpCode(204)
  @ApiOperation({ summary: 'Delete a task' })
  @ApiNoContentResponse()
  @ApiNotFoundResponse()
  remove(@CurrentCaller() caller: Caller, @Param('id', ParseUUIDPipe) id: string): Promise<void> {
    return this.tasks.remove(caller.id, id);
  }
}
