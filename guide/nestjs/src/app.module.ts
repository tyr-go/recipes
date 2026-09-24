import { Module } from '@nestjs/common';
import { AdminModule } from './admin/admin.module';
import { AuthModule } from './auth/auth.module';
import { DatabaseModule } from './database/database.module';
import { HealthModule } from './health/health';
import { ProjectsModule } from './projects/projects.module';
import { TasksModule } from './tasks/tasks.module';

//guide:app-module
@Module({
  imports: [DatabaseModule, AuthModule, ProjectsModule, TasksModule, AdminModule, HealthModule],
})
export class AppModule {}
//guide:end
