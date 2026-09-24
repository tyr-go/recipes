import { ApiProperty, ApiPropertyOptional } from '@nestjs/swagger';
import { Type } from 'class-transformer';
import { IsDate, IsIn, IsInt, IsOptional, IsString, Length, Max, Min } from 'class-validator';

export const STATUSES = ['todo', 'doing', 'done'] as const;
export type Status = (typeof STATUSES)[number];

export class CreateTaskDto {
  @ApiProperty({ description: 'What to do.' })
  @IsString()
  @Length(1, 200)
  title: string;

  @ApiPropertyOptional({ enum: STATUSES, description: 'todo if not set.' })
  @IsOptional()
  @IsIn(STATUSES)
  status?: Status;

  @ApiPropertyOptional({ description: 'When the task is due.' })
  @IsOptional()
  @Type(() => Date)
  @IsDate()
  due_at?: Date;
}

export class ListTasksQuery {
  @ApiPropertyOptional({ enum: STATUSES, description: 'Only the tasks of the status, if set.' })
  @IsOptional()
  @IsIn(STATUSES)
  status?: Status;

  @ApiPropertyOptional({ minimum: 1, maximum: 100, description: 'How many tasks to return: 20 if not set.' })
  @IsOptional()
  @Type(() => Number)
  @IsInt()
  @Min(1)
  @Max(100)
  limit?: number;

  @ApiPropertyOptional({ description: 'The next_cursor of the page before, for the page after it.' })
  @IsOptional()
  @IsString()
  cursor?: string;
}

export class UpdateTaskDto {
  @ApiPropertyOptional()
  @IsOptional()
  @IsString()
  @Length(1, 200)
  title?: string;

  @ApiPropertyOptional({ enum: STATUSES })
  @IsOptional()
  @IsIn(STATUSES)
  status?: Status;

  @ApiPropertyOptional()
  @IsOptional()
  @Type(() => Date)
  @IsDate()
  due_at?: Date;
}

export class TaskDto {
  @ApiProperty({ format: 'uuid' })
  id: string;

  @ApiProperty({ format: 'uuid' })
  project_id: string;

  @ApiProperty({ description: 'The number of the task within its project: 1 for the first one.' })
  number: number;

  @ApiProperty()
  title: string;

  @ApiProperty({ enum: STATUSES })
  status: Status;

  @ApiPropertyOptional()
  due_at?: Date;

  @ApiProperty()
  created_at: Date;

  @ApiProperty()
  updated_at: Date;
}

export class TaskPageDto {
  @ApiProperty({ type: [TaskDto] })
  items: TaskDto[];

  @ApiPropertyOptional()
  next_cursor?: string;
}
