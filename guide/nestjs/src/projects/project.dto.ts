import { ApiProperty, ApiPropertyOptional } from '@nestjs/swagger';
import { Type } from 'class-transformer';
import { IsInt, IsOptional, IsString, Length, Matches, Max, Min } from 'class-validator';

//guide:create-project-dto
export class CreateProjectDto {
  @ApiProperty({ description: 'A short code of the project, of A-Z and 0-9, starting with a letter, such as WEB.' })
  @IsString()
  @Length(2, 10)
  @Matches(/^[A-Z][A-Z0-9]*$/, { message: 'key must be of A-Z and 0-9, starting with a letter' })
  key: string;

  @ApiProperty({ description: 'The name of the project.' })
  @IsString()
  @Length(1, 100)
  name: string;
}
//guide:end

export class ListProjectsQuery {
  @ApiPropertyOptional({ description: 'How many projects to return: 20 if not set.', minimum: 1, maximum: 100 })
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

export class ProjectDto {
  @ApiProperty({ format: 'uuid' })
  id: string;

  @ApiProperty({ description: 'A short code of the project, unique among the projects of its owner, such as WEB.' })
  key: string;

  @ApiProperty()
  name: string;

  @ApiProperty()
  created_at: Date;
}

export class ProjectPageDto {
  @ApiProperty({ type: [ProjectDto] })
  items: ProjectDto[];

  @ApiPropertyOptional()
  next_cursor?: string;
}
