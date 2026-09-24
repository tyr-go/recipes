import { Controller, Get } from '@nestjs/common';
import { ApiBearerAuth, ApiForbiddenResponse, ApiOkResponse, ApiOperation, ApiTags } from '@nestjs/swagger';
import { Roles } from '../auth/decorators';
import { AdminService } from './admin.service';
import { StatsDto } from './stats.dto';

@ApiTags('admin')
@ApiBearerAuth()
@Controller('admin')
export class AdminController {
  constructor(private readonly admin: AdminService) {}

  //guide:stats-route
  @Get('stats')
  @Roles(['admin'])
  @ApiOperation({ summary: 'Count the projects and tasks of all owners' })
  @ApiOkResponse({ type: StatsDto })
  @ApiForbiddenResponse({ description: 'The caller lacks the role admin.' })
  stats(): Promise<StatsDto> {
    return this.admin.stats();
  }
  //guide:end
}
