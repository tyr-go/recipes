import { ApiProperty } from '@nestjs/swagger';

export class StatsDto {
  @ApiProperty()
  projects: number;

  @ApiProperty()
  tasks: number;

  @ApiProperty({ description: 'The number of tasks done.' })
  done: number;
}
