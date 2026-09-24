import { Body, Controller, Delete, Get, HttpCode, Param, ParseUUIDPipe, Post, Query, Res } from '@nestjs/common';
import {
  ApiBearerAuth,
  ApiConflictResponse,
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
import { CreateProjectDto, ListProjectsQuery, ProjectDto, ProjectPageDto } from './project.dto';
import { ProjectsService } from './projects.service';

@ApiTags('projects')
@ApiBearerAuth()
@Controller('projects')
export class ProjectsController {
  constructor(private readonly projects: ProjectsService) {}

  //guide:create-project-route
  @Post()
  @ApiOperation({ summary: 'Create a project' })
  @ApiCreatedResponse({ type: ProjectDto })
  @ApiConflictResponse({ description: 'The caller has a project of the key.' })
  async create(
    @CurrentCaller() caller: Caller,
    @Body() dto: CreateProjectDto,
    @Res({ passthrough: true }) res: Response,
  ): Promise<ProjectDto> {
    const project = await this.projects.create(caller.id, dto);
    res.location(`/projects/${project.id}`);
    return project;
  }
  //guide:end

  @Get(':id')
  @ApiOperation({ summary: 'Get a project' })
  @ApiOkResponse({ type: ProjectDto })
  @ApiNotFoundResponse()
  get(@CurrentCaller() caller: Caller, @Param('id', ParseUUIDPipe) id: string): Promise<ProjectDto> {
    return this.projects.get(caller.id, id);
  }

  @Get()
  @ApiOperation({ summary: 'List the projects of the caller', description: 'Newest first, a page at a time.' })
  @ApiOkResponse({ type: ProjectPageDto })
  list(@CurrentCaller() caller: Caller, @Query() query: ListProjectsQuery): Promise<Page<ProjectDto>> {
    return this.projects.list(caller.id, query);
  }

  @Delete(':id')
  @HttpCode(204)
  @ApiOperation({ summary: 'Delete a project', description: "A project with tasks can't be deleted: delete its tasks first." })
  @ApiNoContentResponse()
  @ApiNotFoundResponse()
  @ApiConflictResponse({ description: 'The project has tasks.' })
  remove(@CurrentCaller() caller: Caller, @Param('id', ParseUUIDPipe) id: string): Promise<void> {
    return this.projects.remove(caller.id, id);
  }
}
