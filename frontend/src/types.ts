export type Vec3 = [number, number, number];
export interface Bounds {
  minX: number;
  maxX: number;
  minY: number;
  maxY: number;
  minZ: number;
  maxZ: number;
}
export interface Player {
  face?: string;
  uuid: string;
  name: string;
  dimension: string;
  position: Vec3;
  rotation: [number, number];
}
export interface WorldInfo {
  path: string;
  name: string;
  version: string;
  dataVersion: number;
  dimensions: string[];
  players: Player[];
  spawn: Vec3;
  spawnDimension?: string;
  minY: number;
  maxY: number;
}
export interface Preview {
  revision: string | number;
  bounds: Bounds;
  blocks: number;
  triangles: number;
  worldTrianglesBefore?: number;
  worldTrianglesAfter?: number;
  optimized?: boolean;
  materials: number;
  warnings: string[];
  origin: Vec3;
}
export interface Settings {
  dimension: string;
  bounds: Bounds;
  resources: string[];
  autoMin: boolean;
  padding: number;
  hollowLeaves: boolean;
  biomeColors: boolean;
  biomeBlend: number;
  players: boolean;
  optimizeMesh: boolean;
}
export interface Status {
  busy: boolean;
  phase: string;
  progress: number;
  error?: string;
  world?: WorldInfo;
  preview?: Preview;
  settings?: Settings;
}
export interface Discovery {
  versions: { name: string; version: string; path: string; launcher: string }[];
  worlds: { name: string; path: string; launcher: string }[];
  warnings: string[];
}
declare global {
  interface Window {
    go?: {
      main: {
        App: {
          ChooseDirectory: () => Promise<string>;
          ChooseFiles: () => Promise<string[]>;
          ChooseSaveFile: () => Promise<string>;
        };
      };
    };
  }
}
